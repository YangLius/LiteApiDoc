package store

import "liteapidoc/internal/model"

// 导入辅助:供 OpenAPI 导入模块做冲突分析。

// ApiExistsByMethodPath 判断同 method+path 接口是否已存在。
func (s *Store) ApiExistsByMethodPath(projectID int64, method, path string) (bool, error) {
	var n int
	err := s.DB.QueryRow("SELECT COUNT(*) FROM apis WHERE project_id=? AND method=? AND path=?",
		projectID, method, path).Scan(&n)
	return n > 0, err
}

// FindApiByMethodPath 按 method+path 查找接口(导入覆盖用)。
func (s *Store) FindApiByMethodPath(projectID int64, method, path string) (model.Api, error) {
	rows, err := s.DB.Query("SELECT "+apiCols+" FROM apis WHERE project_id=? AND method=? AND path=?",
		projectID, method, path)
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

// FindTemplateConflictAmong 在已有接口列表(内存快照)中查找与给定路径产生
// Mock 路由歧义的冲突路径,O(n) 纯内存,批量导入避免逐条全表扫描。
func FindTemplateConflictAmong(existing []model.Api, method, path string) string {
	for i := range existing {
		if existing[i].Method == method && templateAmbiguous(existing[i].Path, path) {
			return existing[i].Path
		}
	}
	return ""
}
