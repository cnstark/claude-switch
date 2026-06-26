package project

import (
	"reflect"
	"testing"
)

// newResolver 构造测试用 resolver：给定项目别名表、直连开关、upstream 名集合。
func newResolver(projects map[string]ProjectRoute, upstreamNames map[string]bool) *ModelResolver {
	return NewResolver(projects, upstreamNames)
}

func TestResolve_AliasSuccess(t *testing.T) {
	r := newResolver(
		map[string]ProjectRoute{
			"project1": {ModelMap: map[string][]string{
				"modelA": {"cfg1", "cfg2"},
				"modelB": {"cfg3"},
			}},
		},
		map[string]bool{"cfg1": true, "cfg2": true, "cfg3": true},
	)
	cfgs, ok := r.Resolve("project1", "modelA")
	if !ok {
		t.Fatal("expected resolve success")
	}
	if !reflect.DeepEqual(cfgs, []string{"cfg1", "cfg2"}) {
		t.Fatalf("expected [cfg1, cfg2], got %v", cfgs)
	}
}

func TestResolve_UnknownProject(t *testing.T) {
	r := newResolver(map[string]ProjectRoute{}, map[string]bool{})
	_, ok := r.Resolve("nonexistent", "modelA")
	if ok {
		t.Fatal("expected resolve failure for unknown project")
	}
}

func TestResolve_UnknownModel_NoDirect(t *testing.T) {
	r := newResolver(
		map[string]ProjectRoute{
			"project1": {ModelMap: map[string][]string{"modelA": {"cfg1"}}},
		},
		map[string]bool{"cfg1": true},
	)
	_, ok := r.Resolve("project1", "modelB")
	if ok {
		t.Fatal("expected resolve failure for unknown model without direct access")
	}
}

func TestResolve_DirectAccess_Hit(t *testing.T) {
	r := newResolver(
		map[string]ProjectRoute{
			"p1": {AllowDirect: true, ModelMap: map[string][]string{"aliasA": {"cfg1"}}},
		},
		map[string]bool{"cfg1": true, "cfg2": true},
	)
	cfgs, ok := r.Resolve("p1", "cfg2")
	if !ok {
		t.Fatal("expected direct access resolve success")
	}
	if !reflect.DeepEqual(cfgs, []string{"cfg2"}) {
		t.Fatalf("expected [cfg2], got %v", cfgs)
	}
}

func TestResolve_DirectAccess_Disabled(t *testing.T) {
	r := newResolver(
		map[string]ProjectRoute{
			"p1": {AllowDirect: false, ModelMap: map[string][]string{"aliasA": {"cfg1"}}},
		},
		map[string]bool{"cfg1": true, "cfg2": true},
	)
	_, ok := r.Resolve("p1", "cfg2")
	if ok {
		t.Fatal("expected resolve failure when direct access disabled and model is cfg name")
	}
}

func TestResolve_DirectAccess_NonUpstreamName(t *testing.T) {
	r := newResolver(
		map[string]ProjectRoute{
			"p1": {AllowDirect: true, ModelMap: map[string][]string{"aliasA": {"cfg1"}}},
		},
		map[string]bool{"cfg1": true},
	)
	_, ok := r.Resolve("p1", "not-a-cfg-name")
	if ok {
		t.Fatal("expected resolve failure for non-alias non-upstream name even with direct access")
	}
}

func TestResolve_AliasPreferredOverDirect(t *testing.T) {
	// 防御性：理论上校验保证别名 ≠ upstream.name，此处验证别名命中路径仍正常工作。
	r := newResolver(
		map[string]ProjectRoute{
			"p1": {AllowDirect: true, ModelMap: map[string][]string{"aliasA": {"cfg1", "cfg2"}}},
		},
		map[string]bool{"cfg1": true, "cfg2": true, "aliasA": true},
	)
	cfgs, ok := r.Resolve("p1", "aliasA")
	if !ok {
		t.Fatal("expected alias resolve success")
	}
	if !reflect.DeepEqual(cfgs, []string{"cfg1", "cfg2"}) {
		t.Fatalf("expected [cfg1, cfg2] from alias, got %v", cfgs)
	}
}
