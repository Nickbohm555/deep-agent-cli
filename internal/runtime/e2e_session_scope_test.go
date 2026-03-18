package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/session"
	sessionpostgres "github.com/Nickbohm555/deep-agent-cli/internal/session/postgres"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/sandbox"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sessionHistoryFixtureThreadID = "11111111-1111-1111-1111-111111111111"

func TestSessionResumePersistsTurnsAcrossStoreRestart(t *testing.T) {
	ctx := context.Background()
	harness := newRuntimeIntegrationHarness(t)
	defer harness.Close()

	initialStore := harness.NewStore(t)

	bootstrap, err := CreateOrResumeSession(ctx, initialStore, SessionLifecycleParams{
		RepoRoot: harness.RepoRoot,
	})
	if err != nil {
		t.Fatalf("CreateOrResumeSession(create) returned error: %v", err)
	}

	firstRunner := NewPersistentTurnRunner(initialStore, stubTurnRunner{
		output: TurnOutput{
			SessionID:     bootstrap.Session.ThreadID,
			AssistantText: "hello back",
			Messages: []Message{
				{Role: MessageRoleUser, Content: "hello"},
				{Role: MessageRoleAssistant, Content: "hello back"},
			},
		},
	})

	if _, err := firstRunner.RunTurn(ctx, TurnInput{
		SessionID:   bootstrap.Session.ThreadID,
		UserMessage: "hello",
	}); err != nil {
		t.Fatalf("first RunTurn returned error: %v", err)
	}

	restartedStore := harness.NewStore(t)
	secondRunner := NewPersistentTurnRunner(restartedStore, stubTurnRunner{
		output: TurnOutput{
			SessionID:     bootstrap.Session.ThreadID,
			AssistantText: "all set",
			Messages: []Message{
				{Role: MessageRoleUser, Content: "hello"},
				{Role: MessageRoleAssistant, Content: "hello back"},
				{Role: MessageRoleUser, Content: "resume me"},
				{Role: MessageRoleAssistant, Content: "all set"},
			},
		},
	})

	if _, err := secondRunner.RunTurn(ctx, TurnInput{
		SessionID:   bootstrap.Session.ThreadID,
		UserMessage: "resume me",
		Conversation: []Message{
			{Role: MessageRoleUser, Content: "hello"},
			{Role: MessageRoleAssistant, Content: "hello back"},
		},
	}); err != nil {
		t.Fatalf("second RunTurn returned error: %v", err)
	}

	resumed, err := CreateOrResumeSession(ctx, restartedStore, SessionLifecycleParams{
		ThreadID: bootstrap.Session.ThreadID,
		RepoRoot: harness.RepoRoot,
	})
	if err != nil {
		t.Fatalf("CreateOrResumeSession(resume) returned error: %v", err)
	}

	if !resumed.Resumed {
		t.Fatal("Resumed = false, want true")
	}
	if resumed.Session.ThreadID != bootstrap.Session.ThreadID {
		t.Fatalf("Session.ThreadID = %q, want %q", resumed.Session.ThreadID, bootstrap.Session.ThreadID)
	}

	want := []struct {
		role    MessageRole
		content string
	}{
		{role: MessageRoleUser, content: "hello"},
		{role: MessageRoleAssistant, content: "hello back"},
		{role: MessageRoleUser, content: "resume me"},
		{role: MessageRoleAssistant, content: "all set"},
	}

	if len(resumed.Messages) != len(want) {
		t.Fatalf("message count = %d, want %d", len(resumed.Messages), len(want))
	}
	if len(resumed.Conversation) != len(want) {
		t.Fatalf("conversation count = %d, want %d", len(resumed.Conversation), len(want))
	}

	var previousID int64
	for i, item := range want {
		if resumed.Messages[i].ID <= previousID {
			t.Fatalf("Messages[%d].ID = %d, want strictly increasing ordering after %d", i, resumed.Messages[i].ID, previousID)
		}
		previousID = resumed.Messages[i].ID

		if resumed.Messages[i].Role != string(item.role) || resumed.Messages[i].Content != item.content {
			t.Fatalf("Messages[%d] = %+v, want %s %q", i, resumed.Messages[i], item.role, item.content)
		}
		if resumed.Conversation[i].Role != item.role || resumed.Conversation[i].Content != item.content {
			t.Fatalf("Conversation[%d] = %+v, want %s %q", i, resumed.Conversation[i], item.role, item.content)
		}
	}
}

func TestSessionHistoryReplayUsesPersistedMessageSequence(t *testing.T) {
	ctx := context.Background()
	harness := newRuntimeIntegrationHarness(t)
	defer harness.Close()

	harness.ApplySQLFile(t, "internal/runtime/testdata/session_fixture.sql", map[string]string{
		"__REPO_ROOT__": sqlStringLiteral(harness.RepoRoot),
	})

	store := harness.NewStore(t)
	bootstrap, err := CreateOrResumeSession(ctx, store, SessionLifecycleParams{
		ThreadID: sessionHistoryFixtureThreadID,
		RepoRoot: harness.RepoRoot,
	})
	if err != nil {
		t.Fatalf("CreateOrResumeSession returned error: %v", err)
	}

	want := []struct {
		id      int64
		role    MessageRole
		content string
	}{
		{id: 7, role: MessageRoleAssistant, content: "assistant-before-user"},
		{id: 8, role: MessageRoleTool, content: "tool-between-turns"},
		{id: 9, role: MessageRoleUser, content: "user-after-tool"},
	}

	if len(bootstrap.Messages) != len(want) {
		t.Fatalf("message count = %d, want %d", len(bootstrap.Messages), len(want))
	}

	for i, item := range want {
		if bootstrap.Messages[i].ID != item.id {
			t.Fatalf("Messages[%d].ID = %d, want %d", i, bootstrap.Messages[i].ID, item.id)
		}
		if bootstrap.Messages[i].Role != string(item.role) || bootstrap.Messages[i].Content != item.content {
			t.Fatalf("Messages[%d] = %+v, want %s %q", i, bootstrap.Messages[i], item.role, item.content)
		}
		if bootstrap.Conversation[i].Role != item.role || bootstrap.Conversation[i].Content != item.content {
			t.Fatalf("Conversation[%d] = %+v, want %s %q", i, bootstrap.Conversation[i], item.role, item.content)
		}
	}

	if !(bootstrap.Messages[0].CreatedAt.After(bootstrap.Messages[1].CreatedAt) &&
		bootstrap.Messages[1].CreatedAt.After(bootstrap.Messages[2].CreatedAt)) {
		t.Fatalf("fixture timestamps were not loaded as expected: %+v", bootstrap.Messages)
	}
}

func TestFreshSessionScopeEnforcesBoundRepo(t *testing.T) {
	ctx := context.Background()
	harness := newRuntimeIntegrationHarness(t)
	defer harness.Close()

	if err := os.WriteFile(filepath.Join(harness.RepoRoot, "inside.txt"), []byte("scoped content"), 0o644); err != nil {
		t.Fatalf("WriteFile inside.txt returned error: %v", err)
	}

	store := harness.NewStore(t)
	bootstrap, err := CreateOrResumeSession(ctx, store, SessionLifecycleParams{
		RepoRoot: harness.RepoRoot,
	})
	if err != nil {
		t.Fatalf("CreateOrResumeSession(create) returned error: %v", err)
	}

	sessionCtx := mustBindRuntimeRepoRoot(t, bootstrap.Session.RepoRoot)
	boundRepoRoot, err := RepoRootFromContext(sessionCtx)
	if err != nil {
		t.Fatalf("RepoRootFromContext returned error: %v", err)
	}

	readScope, err := sandbox.EnforceRepoScope(boundRepoRoot, sandbox.ScopeTarget{
		ToolName:  "read_file",
		Operation: "read",
		Path:      "inside.txt",
	})
	if err != nil {
		t.Fatalf("EnforceRepoScope(read_file inside.txt) returned error: %v", err)
	}
	readContents, err := os.ReadFile(readScope.ResolvedPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", readScope.ResolvedPath, err)
	}
	if string(readContents) != "scoped content" {
		t.Fatalf("inside.txt contents = %q, want scoped content", string(readContents))
	}

	searchScope, err := sandbox.EnforceRepoScope(boundRepoRoot, sandbox.ScopeTarget{
		ToolName:  "code_search",
		Operation: "search",
		Path:      ".",
	})
	if err != nil {
		t.Fatalf("EnforceRepoScope(code_search .) returned error: %v", err)
	}
	searchOutput, err := exec.CommandContext(ctx, "rg", "-n", "scoped", searchScope.ResolvedPath).CombinedOutput()
	if err != nil {
		t.Fatalf("rg returned error: %v; output: %s", err, string(searchOutput))
	}
	if !strings.Contains(string(searchOutput), "inside.txt:1:scoped content") {
		t.Fatalf("rg output = %q, want match inside repo", string(searchOutput))
	}

	_, err = sandbox.EnforceRepoScope(boundRepoRoot, sandbox.ScopeTarget{
		ToolName:  "read_file",
		Operation: "read",
		Path:      "../outside.txt",
	})
	assertScopeDenied(t, err, "read_file read target \"../outside.txt\" denied")
}

func TestResumedSessionScopeKeepsOriginalRepoBinding(t *testing.T) {
	ctx := context.Background()
	harness := newRuntimeIntegrationHarness(t)
	defer harness.Close()

	if err := os.Mkdir(filepath.Join(harness.RepoRoot, "nested"), 0o755); err != nil {
		t.Fatalf("Mkdir nested returned error: %v", err)
	}

	initialStore := harness.NewStore(t)
	bootstrap, err := CreateOrResumeSession(ctx, initialStore, SessionLifecycleParams{
		RepoRoot: harness.RepoRoot,
	})
	if err != nil {
		t.Fatalf("CreateOrResumeSession(create) returned error: %v", err)
	}

	if _, err := initialStore.AppendMessage(ctx, session.AppendMessageParams{
		ThreadID: bootstrap.Session.ThreadID,
		Role:     string(MessageRoleUser),
		Content:  "persist before resume",
	}); err != nil {
		t.Fatalf("AppendMessage returned error: %v", err)
	}

	restartedStore := harness.NewStore(t)
	resumed, err := CreateOrResumeSession(ctx, restartedStore, SessionLifecycleParams{
		ThreadID: bootstrap.Session.ThreadID,
		RepoRoot: harness.RepoRoot,
	})
	if err != nil {
		t.Fatalf("CreateOrResumeSession(resume) returned error: %v", err)
	}
	if !resumed.Resumed {
		t.Fatal("Resumed = false, want true")
	}
	if len(resumed.Messages) != 1 || resumed.Messages[0].Content != "persist before resume" {
		t.Fatalf("resumed messages = %+v, want persisted history before resumed scope checks", resumed.Messages)
	}

	sessionCtx := mustBindRuntimeRepoRoot(t, resumed.Session.RepoRoot)
	boundRepoRoot, err := RepoRootFromContext(sessionCtx)
	if err != nil {
		t.Fatalf("RepoRootFromContext returned error: %v", err)
	}

	listScope, err := sandbox.EnforceRepoScope(boundRepoRoot, sandbox.ScopeTarget{
		ToolName:  "list_files",
		Operation: "list",
		Path:      "nested",
	})
	if err != nil {
		t.Fatalf("EnforceRepoScope(list_files nested) returned error: %v", err)
	}
	files, err := os.ReadDir(listScope.ResolvedPath)
	if err != nil {
		t.Fatalf("ReadDir(%q) returned error: %v", listScope.ResolvedPath, err)
	}
	if len(files) != 0 {
		t.Fatalf("ReadDir(%q) length = %d, want 0", listScope.ResolvedPath, len(files))
	}

	bashScope, err := sandbox.EnforceRepoScope(boundRepoRoot, sandbox.ScopeTarget{
		ToolName:   "bash",
		Operation:  "execute",
		WorkingDir: "nested",
	})
	if err != nil {
		t.Fatalf("EnforceRepoScope(bash nested) returned error: %v", err)
	}

	wantWorkingDir, err := filepath.EvalSymlinks(filepath.Join(harness.RepoRoot, "nested"))
	if err != nil {
		t.Fatalf("EvalSymlinks nested returned error: %v", err)
	}
	cmd := exec.CommandContext(ctx, "bash", "-c", "pwd")
	cmd.Dir = bashScope.WorkingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash pwd in scoped dir returned error: %v; output: %s", err, string(output))
	}
	if strings.TrimSpace(string(output)) != wantWorkingDir {
		t.Fatalf("bash output = %q, want %q", strings.TrimSpace(string(output)), wantWorkingDir)
	}

	_, err = sandbox.EnforceRepoScope(boundRepoRoot, sandbox.ScopeTarget{
		ToolName:   "bash",
		Operation:  "execute",
		WorkingDir: "../outside",
	})
	assertScopeDenied(t, err, "bash execute working directory \"../outside\" denied")
}

func assertScopeDenied(t *testing.T, err error, prefix string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected scope denial error, got nil")
	}
	if !strings.Contains(err.Error(), prefix) {
		t.Fatalf("error = %v, want prefix %q", err, prefix)
	}
	if !strings.Contains(err.Error(), "escapes repository scope") {
		t.Fatalf("error = %v, want repository scope denial", err)
	}
}

func mustBindRuntimeRepoRoot(t *testing.T, repoRoot string) context.Context {
	t.Helper()

	ctx, err := WithRepoRoot(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("WithRepoRoot returned error: %v", err)
	}
	return ctx
}

type runtimeIntegrationHarness struct {
	DatabaseURL string
	SchemaName  string
	RepoRoot    string
	adminPool   *pgxpool.Pool
}

func newRuntimeIntegrationHarness(t *testing.T) *runtimeIntegrationHarness {
	t.Helper()

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for runtime integration tests")
	}

	ctx := context.Background()
	adminPool := newRuntimeTestPool(t, databaseURL, "")

	schemaName := fmt.Sprintf("runtime_e2e_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("CREATE SCHEMA returned error: %v", err)
	}

	harness := &runtimeIntegrationHarness{
		DatabaseURL: databaseURL,
		SchemaName:  schemaName,
		RepoRoot:    mustTempRepoRoot(t),
		adminPool:   adminPool,
	}
	harness.ApplySQLFile(t, "db/migrations/0001_sessions_messages.sql", nil)
	return harness
}

func (h *runtimeIntegrationHarness) Close() {
	if h.adminPool == nil {
		return
	}

	_, _ = h.adminPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+h.SchemaName+" CASCADE")
	h.adminPool.Close()
}

func (h *runtimeIntegrationHarness) NewStore(t *testing.T) *sessionpostgres.Store {
	t.Helper()

	pool := newRuntimeTestPool(t, h.DatabaseURL, h.SchemaName)
	t.Cleanup(pool.Close)
	return sessionpostgres.New(pool)
}

func (h *runtimeIntegrationHarness) ApplySQLFile(t *testing.T, relativePath string, replacements map[string]string) {
	t.Helper()

	contents, err := os.ReadFile(relativePath)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", relativePath, err)
	}

	sql := string(contents)
	for oldValue, newValue := range replacements {
		sql = strings.ReplaceAll(sql, oldValue, newValue)
	}

	if _, err := h.adminPool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("Exec(%q) returned error: %v", relativePath, err)
	}
}

func newRuntimeTestPool(t *testing.T, databaseURL, schemaName string) *pgxpool.Pool {
	t.Helper()

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if schemaName != "" {
		config.ConnConfig.RuntimeParams["search_path"] = schemaName
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("Ping returned error: %v", err)
	}

	return pool
}

func mustTempRepoRoot(t *testing.T) string {
	t.Helper()

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repoRoot, 0o755); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	return repoRoot
}

func sqlStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
