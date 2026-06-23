package project

// ModelResolver 按项目名 + 请求模型名查找有序 cfg 列表
type ModelResolver map[string]map[string][]string

// NewResolver 从配置数据创建查表器
func NewResolver(projects map[string]map[string][]string) ModelResolver {
	return ModelResolver(projects)
}

// Resolve 返回项目下某请求模型对应的有序 cfg 名列表
func (r ModelResolver) Resolve(projectName, requestModel string) ([]string, bool) {
	proj, ok := r[projectName]
	if !ok {
		return nil, false
	}
	cfgs, ok := proj[requestModel]
	if !ok || len(cfgs) == 0 {
		return nil, false
	}
	return cfgs, true
}
