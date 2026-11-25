// Package store 封装 SQLite 访问与全部数据操作。
// MVP 策略:单连接串行化写操作,规避 SQLite 锁竞争;小团队规模足够。
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动,无 CGO

	"liteapidoc/internal/model"
)

// Store 数据存储句柄。
type Store struct {
	DB *sql.DB
}

// ErrNotFound 统一未找到错误。
var ErrNotFound = errors.New("not found")

// ErrConflict method+path 唯一性冲突。
var ErrConflict = errors.New("duplicate method+path")

// ErrTemplateConflict Mock 路由模板冲突(无法消歧)。
var ErrTemplateConflict = errors.New("mock route template conflict")

// Open 打开(或创建)数据库并执行迁移。
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 单写连接,彻底避免 SQLITE_BUSY;读多写少场景性能足够。
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS settings (
			k TEXT PRIMARY KEY,
			v TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			tags TEXT NOT NULL DEFAULT '[]',
			access_pwd_hash TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			mock_enabled INTEGER NOT NULL DEFAULT 1,
			global_headers TEXT NOT NULL DEFAULT '[]',
			global_mock_headers TEXT NOT NULL DEFAULT '[]',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			parent_id INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS apis (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			group_id INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'developing',
			owner TEXT NOT NULL DEFAULT '',
			remark TEXT NOT NULL DEFAULT '',
			path_params TEXT NOT NULL DEFAULT '[]',
			query_params TEXT NOT NULL DEFAULT '[]',
			header_params TEXT NOT NULL DEFAULT '[]',
			body_schema TEXT NOT NULL DEFAULT '',
			body_example TEXT NOT NULL DEFAULT '',
			responses TEXT NOT NULL DEFAULT '[]',
			mock_config TEXT NOT NULL DEFAULT '{}',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0,
			UNIQUE(project_id, method, path)
		)`,
		`CREATE TABLE IF NOT EXISTS envs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS mock_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			query TEXT NOT NULL DEFAULT '',
			resp_code INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mock_logs_project ON mock_logs(project_id, id)`,
		`CREATE TABLE IF NOT EXISTS shares (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL UNIQUE,
			token TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL DEFAULT 0
		)`,
	}
	for _, q := range stmts {
		if _, err := s.DB.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// 增量列迁移(SQLite 无 IF NOT EXISTS 列语法,重复执行报 duplicate column,忽略)
	for _, q := range []string{
		`ALTER TABLE projects ADD COLUMN spec_url TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := s.DB.Exec(q); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migrate alter: %w", err)
		}
	}
	return nil
}

// BackupTo 使用 VACUUM INTO 生成一致性快照副本。
func (s *Store) BackupTo(dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	_, err := s.DB.Exec("VACUUM INTO ?", dest)
	return err
}

// ---- settings ----

func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.DB.QueryRow("SELECT v FROM settings WHERE k=?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(key, val string) error {
	_, err := s.DB.Exec(`INSERT INTO settings(k,v) VALUES(?,?)
		ON CONFLICT(k) DO UPDATE SET v=excluded.v`, key, val)
	return err
}

func (s *Store) AllSettings() (map[string]string, error) {
	rows, err := s.DB.Query("SELECT k,v FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// ---- helpers ----

func nowUnix() int64 { return time.Now().Unix() }

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func scanProjects(rows *sql.Rows) ([]model.Project, error) {
	var out []model.Project
	for rows.Next() {
		var p model.Project
		var tags, gh, gmh string
		var pwd string
		var archived, mockEnabled int
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &tags, &pwd, &archived,
			&mockEnabled, &gh, &gmh, &p.SpecUrl, &p.SortOrder, &p.CreatedAt, &p.UpdatedAt, &p.ApiCount); err != nil {
			return nil, err
		}
		p.HasPassword = pwd != ""
		p.Archived = archived == 1
		p.MockEnabled = mockEnabled == 1
		_ = json.Unmarshal([]byte(tags), &p.Tags)
		_ = json.Unmarshal([]byte(gh), &p.GlobalHeaders)
		_ = json.Unmarshal([]byte(gmh), &p.GlobalMockHeaders)
		if p.Tags == nil {
			p.Tags = []string{}
		}
		if p.GlobalHeaders == nil {
			p.GlobalHeaders = []model.KV{}
		}
		if p.GlobalMockHeaders == nil {
			p.GlobalMockHeaders = []model.KV{}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const projectCols = `id,name,description,tags,access_pwd_hash,archived,mock_enabled,global_headers,global_mock_headers,spec_url,sort_order,created_at,updated_at,
 (SELECT COUNT(*) FROM apis a WHERE a.project_id=projects.id) AS api_count`

// GetProject 查询单个项目。
func (s *Store) GetProject(id int64) (model.Project, error) {
	rows, err := s.DB.Query("SELECT "+projectCols+" FROM projects WHERE id=?", id)
	if err != nil {
		return model.Project{}, err
	}
	defer rows.Close()
	list, err := scanProjects(rows)
	if err != nil {
		return model.Project{}, err
	}
	if len(list) == 0 {
		return model.Project{}, ErrNotFound
	}
	return list[0], nil
}

// ListProjects 项目列表(按 sort_order,id)。
func (s *Store) ListProjects() ([]model.Project, error) {
	rows, err := s.DB.Query("SELECT " + projectCols + " FROM projects ORDER BY sort_order DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list, err := scanProjects(rows)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []model.Project{}
	}
	return list, nil
}

// CreateProject 新建项目。
func (s *Store) CreateProject(p *model.Project, pwdHash string) error {
	now := nowUnix()
	res, err := s.DB.Exec(`INSERT INTO projects
		(name,description,tags,access_pwd_hash,archived,mock_enabled,global_headers,global_mock_headers,spec_url,sort_order,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.Name, p.Description, mustJSON(p.Tags), pwdHash, boolInt(p.Archived), boolInt(p.MockEnabled),
		mustJSON(p.GlobalHeaders), mustJSON(p.GlobalMockHeaders), p.SpecUrl, p.SortOrder, now, now)
	if err != nil {
		return err
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	p.CreatedAt, p.UpdatedAt = now, now
	p.HasPassword = pwdHash != ""
	return nil
}

// UpdateProject 更新项目基础信息(全量字段)。
func (s *Store) UpdateProject(p *model.Project) error {
	_, err := s.DB.Exec(`UPDATE projects SET name=?,description=?,tags=?,archived=?,mock_enabled=?,
		global_headers=?,global_mock_headers=?,spec_url=?,sort_order=?,updated_at=? WHERE id=?`,
		p.Name, p.Description, mustJSON(p.Tags), boolInt(p.Archived), boolInt(p.MockEnabled),
		mustJSON(p.GlobalHeaders), mustJSON(p.GlobalMockHeaders), p.SpecUrl, p.SortOrder, nowUnix(), p.ID)
	return err
}

// SetProjectPassword 设置/清除项目访问密码(空串清除)。
func (s *Store) SetProjectPassword(id int64, hash string) error {
	_, err := s.DB.Exec("UPDATE projects SET access_pwd_hash=?,updated_at=? WHERE id=?", hash, nowUnix(), id)
	return err
}

// GetProjectPasswordHash 读取密码哈希用于校验。
func (s *Store) GetProjectPasswordHash(id int64) (string, error) {
	var h string
	err := s.DB.QueryRow("SELECT access_pwd_hash FROM projects WHERE id=?", id).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return h, err
}

// DeleteProject 删除项目及全部关联数据。
func (s *Store) DeleteProject(id int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		"DELETE FROM apis WHERE project_id=?", "DELETE FROM groups WHERE project_id=?",
		"DELETE FROM envs WHERE project_id=?", "DELETE FROM mock_logs WHERE project_id=?",
		"DELETE FROM shares WHERE project_id=?", "DELETE FROM projects WHERE id=?",
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearAllData 管理员清空全部业务数据(保留设置)。
func (s *Store) ClearAllData() error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		"DELETE FROM apis", "DELETE FROM groups", "DELETE FROM envs",
		"DELETE FROM mock_logs", "DELETE FROM shares", "DELETE FROM projects",
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- groups ----

func (s *Store) ListGroups(projectID int64) ([]model.Group, error) {
	rows, err := s.DB.Query(`SELECT id,project_id,name,parent_id,sort_order,created_at,updated_at
		FROM groups WHERE project_id=? ORDER BY sort_order,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Group
	for rows.Next() {
		var g model.Group
		if err := rows.Scan(&g.ID, &g.ProjectID, &g.Name, &g.ParentID, &g.SortOrder, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) CreateGroup(g *model.Group) error {
	now := nowUnix()
	res, err := s.DB.Exec(`INSERT INTO groups(project_id,name,parent_id,sort_order,created_at,updated_at)
		VALUES(?,?,?,?,?,?)`, g.ProjectID, g.Name, g.ParentID, g.SortOrder, now, now)
	if err != nil {
		return err
	}
	if g.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	g.CreatedAt, g.UpdatedAt = now, now
	return nil
}

// GetGroup 查询分组。
func (s *Store) GetGroup(id int64) (model.Group, error) {
	var g model.Group
	err := s.DB.QueryRow(`SELECT id,project_id,name,parent_id,sort_order,created_at,updated_at
		FROM groups WHERE id=?`, id).
		Scan(&g.ID, &g.ProjectID, &g.Name, &g.ParentID, &g.SortOrder, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

func (s *Store) UpdateGroup(g *model.Group) error {
	_, err := s.DB.Exec(`UPDATE groups SET name=?,parent_id=?,sort_order=?,updated_at=? WHERE id=?`,
		g.Name, g.ParentID, g.SortOrder, nowUnix(), g.ID)
	return err
}

// DeleteGroup 删除分组:子分组一并删除,组内接口移至未分组。
func (s *Store) DeleteGroup(id int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE apis SET group_id=0 WHERE group_id IN (SELECT id FROM groups WHERE id=? OR parent_id=?)", id, id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM groups WHERE id=? OR parent_id=?", id, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- apis ----

func scanApis(rows *sql.Rows) ([]model.Api, error) {
	var out []model.Api
	for rows.Next() {
		var a model.Api
		var pp, qp, hp, bs, be, resp, mc string
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.GroupID, &a.Name, &a.Method, &a.Path, &a.Status,
			&a.Owner, &a.Remark, &pp, &qp, &hp, &bs, &be, &resp, &mc, &a.SortOrder, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(pp), &a.PathParams)
		_ = json.Unmarshal([]byte(qp), &a.QueryParams)
		_ = json.Unmarshal([]byte(hp), &a.HeaderParams)
		if bs != "" {
			a.BodySchema = json.RawMessage(bs)
		}
		a.BodyExample = be
		_ = json.Unmarshal([]byte(resp), &a.Responses)
		_ = json.Unmarshal([]byte(mc), &a.Mock)
		// 保证非 null
		if a.PathParams == nil {
			a.PathParams = []model.Param{}
		}
		if a.QueryParams == nil {
			a.QueryParams = []model.Param{}
		}
		if a.HeaderParams == nil {
			a.HeaderParams = []model.HeaderParam{}
		}
		if a.Responses == nil {
			a.Responses = []model.ResponseDef{}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const apiCols = `id,project_id,group_id,name,method,path,status,owner,remark,path_params,query_params,header_params,body_schema,body_example,responses,mock_config,sort_order,created_at,updated_at`

// ListApis 项目内全部接口。
func (s *Store) ListApis(projectID int64) ([]model.Api, error) {
	rows, err := s.DB.Query("SELECT "+apiCols+" FROM apis WHERE project_id=? ORDER BY sort_order,id", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list, err := scanApis(rows)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []model.Api{}
	}
	return list, nil
}

// GetApi 查询接口。
func (s *Store) GetApi(id int64) (model.Api, error) {
	rows, err := s.DB.Query("SELECT "+apiCols+" FROM apis WHERE id=?", id)
	if err != nil {
		return model.Api{}, err
	}
	defer rows.Close()
	list, err := scanApis(rows)
	if err != nil {
		return model.Api{}, err
	}
	if len(list) == 0 {
		return model.Api{}, ErrNotFound
	}
	return list[0], nil
}

func (s *Store) apiExists(projectID int64, method, path string, excludeID int64) (bool, error) {
	var n int
	err := s.DB.QueryRow("SELECT COUNT(*) FROM apis WHERE project_id=? AND method=? AND path=? AND id<>?",
		projectID, method, path, excludeID).Scan(&n)
	return n > 0, err
}

// CreateApi 新建接口,含唯一性与模板冲突校验。
func (s *Store) CreateApi(a *model.Api) error {
	dup, err := s.apiExists(a.ProjectID, a.Method, a.Path, 0)
	if err != nil {
		return err
	}
	if dup {
		return ErrConflict
	}
	existing, err := s.ListApis(a.ProjectID)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.Method == a.Method && templateAmbiguous(e.Path, a.Path) {
			return ErrTemplateConflict
		}
	}
	now := nowUnix()
	res, err := s.DB.Exec(`INSERT INTO apis
		(project_id,group_id,name,method,path,status,owner,remark,path_params,query_params,header_params,body_schema,body_example,responses,mock_config,sort_order,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ProjectID, a.GroupID, a.Name, a.Method, a.Path, a.Status, a.Owner, a.Remark,
		mustJSON(a.PathParams), mustJSON(a.QueryParams), mustJSON(a.HeaderParams),
		string(a.BodySchema), a.BodyExample, mustJSON(a.Responses), mustJSON(a.Mock), a.SortOrder, now, now)
	if err != nil {
		return err
	}
	a.ID, _ = res.LastInsertId()
	a.CreatedAt, a.UpdatedAt = now, now
	return nil
}

// UpdateApi 更新接口(全量),校验排除自身。
func (s *Store) UpdateApi(a *model.Api) error {
	dup, err := s.apiExists(a.ProjectID, a.Method, a.Path, a.ID)
	if err != nil {
		return err
	}
	if dup {
		return ErrConflict
	}
	existing, err := s.ListApis(a.ProjectID)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.ID != a.ID && e.Method == a.Method && templateAmbiguous(e.Path, a.Path) {
			return ErrTemplateConflict
		}
	}
	_, err = s.DB.Exec(`UPDATE apis SET project_id=?,group_id=?,name=?,method=?,path=?,status=?,owner=?,remark=?,
		path_params=?,query_params=?,header_params=?,body_schema=?,body_example=?,responses=?,mock_config=?,sort_order=?,updated_at=?
		WHERE id=?`,
		a.ProjectID, a.GroupID, a.Name, a.Method, a.Path, a.Status, a.Owner, a.Remark,
		mustJSON(a.PathParams), mustJSON(a.QueryParams), mustJSON(a.HeaderParams),
		string(a.BodySchema), a.BodyExample, mustJSON(a.Responses), mustJSON(a.Mock), a.SortOrder, nowUnix(), a.ID)
	return err
}

// UpdateApiMockConfig 仅更新 Mock 策略(文档页快捷开关)。
func (s *Store) UpdateApiMockConfig(id int64, mc model.MockConfig) error {
	_, err := s.DB.Exec("UPDATE apis SET mock_config=?,updated_at=? WHERE id=?", mustJSON(mc), nowUnix(), id)
	return err
}

func (s *Store) DeleteApi(id int64) error {
	_, err := s.DB.Exec("DELETE FROM apis WHERE id=?", id)
	return err
}

// BatchSortApis 批量更新接口的分组与排序(拖拽)。
func (s *Store) BatchSortApis(items []struct {
	ID        int64 `json:"id"`
	GroupID   int64 `json:"groupId"`
	SortOrder int64 `json:"sortOrder"`
}) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := nowUnix()
	for _, it := range items {
		if _, err := tx.Exec("UPDATE apis SET group_id=?,sort_order=?,updated_at=? WHERE id=?",
			it.GroupID, it.SortOrder, now, it.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// templateAmbiguous 判断两个路径模板是否会产生无法消歧的 Mock 路由:
// 段数相同、占位符位置相同、且全部固定段内容一致(此时任意具体 URL 同时命中两者)。
// 完全相同的字面路径由唯一性约束另行拦截。
func templateAmbiguous(a, b string) bool {
	as := splitPath(a)
	bs := splitPath(b)
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		aPh, bPh := isPlaceholder(as[i]), isPlaceholder(bs[i])
		if aPh != bPh {
			return false
		}
		if !aPh && as[i] != bs[i] {
			return false
		}
	}
	// 占位符位置一致且固定段一致 → 有歧义;但两条完全相同的路径归唯一性约束管。
	return a != b
}

func splitPath(p string) []string {
	out := []string{}
	cur := ""
	for _, r := range p {
		if r == '/' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func isPlaceholder(seg string) bool {
	return len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}

// ---- envs ----

func (s *Store) ListEnvs(projectID int64) ([]model.Env, error) {
	rows, err := s.DB.Query(`SELECT id,project_id,name,base_url,sort_order,created_at,updated_at
		FROM envs WHERE project_id=? ORDER BY sort_order,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Env
	for rows.Next() {
		var e model.Env
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Name, &e.BaseURL, &e.SortOrder, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) CreateEnv(e *model.Env) error {
	now := nowUnix()
	res, err := s.DB.Exec(`INSERT INTO envs(project_id,name,base_url,sort_order,created_at,updated_at)
		VALUES(?,?,?,?,?,?)`, e.ProjectID, e.Name, e.BaseURL, e.SortOrder, now, now)
	if err != nil {
		return err
	}
	if e.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	e.CreatedAt, e.UpdatedAt = now, now
	return nil
}

func (s *Store) GetEnv(id int64) (model.Env, error) {
	var e model.Env
	err := s.DB.QueryRow(`SELECT id,project_id,name,base_url,sort_order,created_at,updated_at
		FROM envs WHERE id=?`, id).
		Scan(&e.ID, &e.ProjectID, &e.Name, &e.BaseURL, &e.SortOrder, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

func (s *Store) UpdateEnv(e *model.Env) error {
	_, err := s.DB.Exec(`UPDATE envs SET name=?,base_url=?,sort_order=?,updated_at=? WHERE id=?`,
		e.Name, e.BaseURL, e.SortOrder, nowUnix(), e.ID)
	return err
}

func (s *Store) DeleteEnv(id int64) error {
	_, err := s.DB.Exec("DELETE FROM envs WHERE id=?", id)
	return err
}

// ---- mock logs ----

// InsertMockLog 写入日志并裁剪到最近 100 条(项目级)。
func (s *Store) InsertMockLog(l *model.MockLog) error {
	l.CreatedAt = nowUnix()
	if _, err := s.DB.Exec(`INSERT INTO mock_logs(project_id,method,path,query,resp_code,duration_ms,created_at)
		VALUES(?,?,?,?,?,?,?)`, l.ProjectID, l.Method, l.Path, l.Query, l.RespCode, l.DurationMS, l.CreatedAt); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM mock_logs WHERE project_id=? AND id NOT IN
		(SELECT id FROM mock_logs WHERE project_id=? ORDER BY id DESC LIMIT 100)`, l.ProjectID, l.ProjectID)
	return err
}

func (s *Store) ListMockLogs(projectID int64, limit int) ([]model.MockLog, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT id,project_id,method,path,query,resp_code,duration_ms,created_at
		FROM mock_logs WHERE project_id=? ORDER BY id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.MockLog
	for rows.Next() {
		var l model.MockLog
		if err := rows.Scan(&l.ID, &l.ProjectID, &l.Method, &l.Path, &l.Query, &l.RespCode, &l.DurationMS, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) ClearMockLogs(projectID int64) error {
	_, err := s.DB.Exec("DELETE FROM mock_logs WHERE project_id=?", projectID)
	return err
}

// ---- shares ----

func (s *Store) GetShareByProject(projectID int64) (model.Share, error) {
	var sh model.Share
	var pwd string
	var enabled int
	err := s.DB.QueryRow(`SELECT id,project_id,token,password_hash,enabled,created_at
		FROM shares WHERE project_id=?`, projectID).
		Scan(&sh.ID, &sh.ProjectID, &sh.Token, &pwd, &enabled, &sh.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return sh, ErrNotFound
	}
	sh.HasPassword = pwd != ""
	sh.Enabled = enabled == 1
	return sh, err
}

func (s *Store) GetShareByToken(token string) (model.Share, error) {
	var sh model.Share
	var pwd string
	var enabled int
	err := s.DB.QueryRow(`SELECT id,project_id,token,password_hash,enabled,created_at
		FROM shares WHERE token=?`, token).
		Scan(&sh.ID, &sh.ProjectID, &sh.Token, &pwd, &enabled, &sh.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return sh, ErrNotFound
	}
	sh.HasPassword = pwd != ""
	sh.Enabled = enabled == 1
	return sh, err
}

func (s *Store) GetSharePasswordHash(projectID int64) (string, error) {
	var h string
	err := s.DB.QueryRow("SELECT password_hash FROM shares WHERE project_id=?", projectID).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return h, err
}

// UpsertShare 创建或更新分享配置。
func (s *Store) UpsertShare(sh *model.Share, pwdHash string) error {
	now := nowUnix()
	_, err := s.DB.Exec(`INSERT INTO shares(project_id,token,password_hash,enabled,created_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(project_id) DO UPDATE SET token=excluded.token,password_hash=excluded.password_hash,enabled=excluded.enabled`,
		sh.ProjectID, sh.Token, pwdHash, boolInt(sh.Enabled), now)
	return err
}

func (s *Store) DeleteShare(projectID int64) error {
	_, err := s.DB.Exec("DELETE FROM shares WHERE project_id=?", projectID)
	return err
}
