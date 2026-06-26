package project

// ProjectRoute 单个项目的路由数据。
// ModelMap 是「请求模型名别名 → 有序 cfg 名列表」映射；
// AllowDirect 开启后，请求模型名若等于某 upstream.name 也可直接路由到该 upstream。
type ProjectRoute struct {
	ModelMap    map[string][]string
	AllowDirect bool
}

// ModelResolver 按项目名 + 请求模型名查找有序 cfg 名列表。
// 别名查表优先；项目开启直连时，请求模型名命中 upstream 名集合则回退直连。
// 校验保证别名 ≠ upstream.name，两条路径不会同时命中同一名。
type ModelResolver struct {
	projects      map[string]ProjectRoute
	upstreamNames map[string]bool // cfg name 集合，用于直连匹配
}

// NewResolver 从配置数据创建查表器。
// projects: 项目名 → ProjectRoute；upstreamNames: 全部 upstream.name 集合。
func NewResolver(projects map[string]ProjectRoute, upstreamNames map[string]bool) *ModelResolver {
	return &ModelResolver{projects: projects, upstreamNames: upstreamNames}
}

// Resolve 返回项目下某请求模型对应的有序 cfg 名列表。
// 1. 别名命中 model_map → 返回该列表
// 2. 项目开启直连且请求模型名是某 upstream.name → 返回 [请求模型名]
// 否则 miss。
func (r *ModelResolver) Resolve(projectName, requestModel string) ([]string, bool) {
	proj, ok := r.projects[projectName]
	if !ok {
		return nil, false
	}
	if cfgs, ok := proj.ModelMap[requestModel]; ok && len(cfgs) > 0 {
		return cfgs, true
	}
	if proj.AllowDirect && r.upstreamNames[requestModel] {
		return []string{requestModel}, true
	}
	return nil, false
}
