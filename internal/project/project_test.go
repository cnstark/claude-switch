package project

import (
	"testing"
)

func TestResolve_Success(t *testing.T) {
	p := ModelResolver{
		"project1": {
			"modelA": {"cfg1", "cfg2"},
			"modelB": {"cfg3"},
		},
	}
	cfgs, ok := p.Resolve("project1", "modelA")
	if !ok {
		t.Fatal("expected resolve success")
	}
	if len(cfgs) != 2 {
		t.Fatalf("expected 2 cfgs, got %d", len(cfgs))
	}
	if cfgs[0] != "cfg1" || cfgs[1] != "cfg2" {
		t.Fatalf("expected [cfg1, cfg2], got %v", cfgs)
	}
}

func TestResolve_UnknownProject(t *testing.T) {
	p := ModelResolver{}
	_, ok := p.Resolve("nonexistent", "modelA")
	if ok {
		t.Fatal("expected resolve failure for unknown project")
	}
}

func TestResolve_UnknownModel(t *testing.T) {
	p := ModelResolver{
		"project1": {
			"modelA": {"cfg1"},
		},
	}
	_, ok := p.Resolve("project1", "modelB")
	if ok {
		t.Fatal("expected resolve failure for unknown model")
	}
}

func TestResolverFromConfig(t *testing.T) {
	projects := map[string]map[string][]string{
		"p1": {"mA": {"cfg1"}},
		"p2": {"mB": {"cfg2", "cfg3"}},
	}
	r := NewResolver(projects)
	cfgs, ok := r.Resolve("p2", "mB")
	if !ok {
		t.Fatal("expected resolve success")
	}
	if len(cfgs) != 2 {
		t.Fatalf("expected 2 cfgs, got %d", len(cfgs))
	}
}
