package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDefaults_SchemaSubdirPresent(t *testing.T) {
	cwd := t.TempDir()
	mustMkdir(t, filepath.Join(cwd, "schema"))

	cfg := resolveDefaults(cwd, defaults{})
	if cfg.SchemaDir != filepath.Join(cwd, "schema") {
		t.Fatalf("expected -schema-dir = CWD/schema, got %q", cfg.SchemaDir)
	}
	if cfg.EntityDir != cwd {
		t.Fatalf("expected -entity-dir = CWD (schema is in subdir), got %q", cfg.EntityDir)
	}
}

func TestResolveDefaults_SchemaLocal(t *testing.T) {
	cwd := t.TempDir() // no ./schema/ subdir

	cfg := resolveDefaults(cwd, defaults{})
	if cfg.SchemaDir != cwd {
		t.Fatalf("expected -schema-dir = CWD, got %q", cfg.SchemaDir)
	}
	expectedEntity := filepath.Join(cwd, "entity")
	if cfg.EntityDir != expectedEntity {
		t.Fatalf("expected -entity-dir = CWD/entity, got %q", cfg.EntityDir)
	}
}

func TestResolveDefaults_ExplicitSchemaDirEqualsCWD(t *testing.T) {
	cwd := t.TempDir()
	mustMkdir(t, filepath.Join(cwd, "schema")) // present but should be IGNORED since explicit flag given

	cfg := resolveDefaults(cwd, defaults{schemaDirExplicit: cwd})
	if cfg.SchemaDir != cwd {
		t.Fatalf("expected explicit -schema-dir to win, got %q", cfg.SchemaDir)
	}
	if cfg.EntityDir != filepath.Join(cwd, "entity") {
		t.Fatalf("explicit -schema-dir = CWD must trigger -entity-dir = CWD/entity, got %q", cfg.EntityDir)
	}
}

func TestResolveDefaults_ExplicitSchemaDirElsewhere(t *testing.T) {
	cwd := t.TempDir()
	mytypes := filepath.Join(cwd, "mytypes")
	mustMkdir(t, mytypes)

	cfg := resolveDefaults(cwd, defaults{schemaDirExplicit: mytypes})
	if cfg.SchemaDir != mytypes {
		t.Fatalf("expected explicit -schema-dir to win, got %q", cfg.SchemaDir)
	}
	if cfg.EntityDir != cwd {
		t.Fatalf("explicit -schema-dir != CWD must yield -entity-dir = CWD, got %q", cfg.EntityDir)
	}
}

func TestResolveDefaults_ClientDirsFollowParents(t *testing.T) {
	// When -schema-client-dir / -entity-client-dir are not explicitly set,
	// they default to the same paths as their parents.
	cwd := t.TempDir()
	mustMkdir(t, filepath.Join(cwd, "schema"))

	cfg := resolveDefaults(cwd, defaults{})
	if cfg.SchemaClientDir != cfg.SchemaDir {
		t.Fatalf("expected -schema-client-dir = -schema-dir by default, got %q vs %q", cfg.SchemaClientDir, cfg.SchemaDir)
	}
	if cfg.EntityClientDir != cfg.EntityDir {
		t.Fatalf("expected -entity-client-dir = -entity-dir by default, got %q vs %q", cfg.EntityClientDir, cfg.EntityDir)
	}
}

func TestResolveDefaults_ClientDirsExplicit(t *testing.T) {
	cwd := t.TempDir()
	mustMkdir(t, filepath.Join(cwd, "schema"))
	apiDir := filepath.Join(cwd, "api")
	mustMkdir(t, apiDir)

	cfg := resolveDefaults(cwd, defaults{
		schemaClientDirExplicit: apiDir,
		entityClientDirExplicit: apiDir,
	})
	if cfg.SchemaClientDir != apiDir {
		t.Fatalf("expected explicit -schema-client-dir to win, got %q", cfg.SchemaClientDir)
	}
	if cfg.EntityClientDir != apiDir {
		t.Fatalf("expected explicit -entity-client-dir to win, got %q", cfg.EntityClientDir)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
