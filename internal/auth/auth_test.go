package auth

import (
	"testing"
)

func TestStore_Authenticate_Success(t *testing.T) {
	s := NewStore(map[string]string{
		"sk-cs-key1": "project1",
		"sk-cs-key2": "project2",
	})
	proj, ok := s.Authenticate("sk-cs-key1")
	if !ok {
		t.Fatal("expected authentication success")
	}
	if proj != "project1" {
		t.Fatalf("expected project1, got %q", proj)
	}
}

func TestStore_Authenticate_UnknownKey(t *testing.T) {
	s := NewStore(map[string]string{
		"sk-cs-key1": "project1",
	})
	_, ok := s.Authenticate("sk-cs-badkey")
	if ok {
		t.Fatal("expected authentication failure for unknown key")
	}
}

func TestStore_Authenticate_EmptyKey(t *testing.T) {
	s := NewStore(map[string]string{
		"sk-cs-key1": "project1",
	})
	_, ok := s.Authenticate("")
	if ok {
		t.Fatal("expected authentication failure for empty key")
	}
}

func TestStore_Authenticate_ConstantTime(t *testing.T) {
	// 验证相同长度的不同 key 不会通过
	s := NewStore(map[string]string{
		"sk-cs-key1project1longer": "project1",
	})
	_, ok := s.Authenticate("sk-cs-key2project2longer")
	if ok {
		t.Fatal("different keys of same length should not authenticate")
	}
}
