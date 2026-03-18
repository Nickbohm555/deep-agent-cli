package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
	_ "unsafe"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexstore"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	indexstatus "github.com/Nickbohm555/deep-agent-cli/internal/indexsync/status"
	"github.com/Nickbohm555/deep-agent-cli/internal/retrieval"
	"github.com/Nickbohm555/deep-agent-cli/internal/runtime"
	"github.com/Nickbohm555/deep-agent-cli/internal/session"
	"github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:linkname linkedNewSemanticSearchPool github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers.newSemanticSearchPool
var linkedNewSemanticSearchPool func(context.Context) (*pgxpool.Pool, error)

//go:linkname linkedNewSemanticRetriever github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers.newSemanticRetriever
var linkedNewSemanticRetriever func(*pgxpool.Pool) (retrieval.SemanticRetriever, error)

//go:linkname linkedCloseSemanticSearchPool github.com/Nickbohm555/deep-agent-cli/internal/tools/handlers.closeSemanticSearchPool
var linkedCloseSemanticSearchPool func(*pgxpool.Pool)

var retrievalLinePattern = regexp.MustCompile(`^(\d+)\. (.+) \| (.+) \| score=([0-9]+\.[0-9]+)$`)

func TestSemanticVerticalSliceE2E(t *testing.T) {
	repoRoot := seedSemanticFixtureRepo(t)
	store := &stubSessionStore{
		sessionID: "semantic-slice-session",
		now:       time.Date(2026, time.March, 18, 15, 4, 5, 0, time.UTC),
	}

	bootstrap, err := runtime.CreateOrResumeSession(context.Background(), store, runtime.SessionLifecycleParams{
		RepoRoot: repoRoot,
	})
	if err != nil {
		t.Fatalf("CreateOrResumeSession returned error: %v", err)
	}
	if bootstrap.Session.ThreadID != store.sessionID {
		t.Fatalf("session thread_id = %q, want %q", bootstrap.Session.ThreadID, store.sessionID)
	}
	if bootstrap.Session.RepoRoot == "" {
		t.Fatal("session repo_root is empty")
	}
	if store.createdRepoRoot != bootstrap.Session.RepoRoot {
		t.Fatalf("created repo_root = %q, want %q", store.createdRepoRoot, bootstrap.Session.RepoRoot)
	}

	workflow := newFakeIndexWorkflow(store.sessionID, bootstrap.Session.RepoRoot)
	statusService := indexstatus.NewService(workflow, workflow)
	waitForIndexReady(t, statusService, workflow, store.sessionID, bootstrap.Session.RepoRoot)

	retriever := newFixtureSemanticRetriever(t, workflow, store.sessionID, bootstrap.Session.RepoRoot, bootstrap.Session.RepoRoot)
	restoreSemanticSearchFactories(t)
	linkedNewSemanticSearchPool = func(context.Context) (*pgxpool.Pool, error) {
		return &pgxpool.Pool{}, nil
	}
	linkedCloseSemanticSearchPool = func(*pgxpool.Pool) {}
	linkedNewSemanticRetriever = func(*pgxpool.Pool) (retrieval.SemanticRetriever, error) {
		return retriever, nil
	}

	ctx := bindSessionScope(t, bootstrap.Session.ThreadID, bootstrap.Session.RepoRoot)
	query := "where is semantic retrieval evidence documented and injected into the runtime prompt?"

	toolResult, err := handlers.SemanticSearch(ctx, semanticSearchToolCall(t, handlers.SemanticSearchInput{
		Query: query,
		TopK:  3,
	}))
	if err != nil {
		t.Fatalf("SemanticSearch returned error: %v", err)
	}
	if toolResult.IsError {
		t.Fatalf("SemanticSearch unexpectedly marked result as error: %s", toolResult.Output)
	}

	var output semanticSearchOutput
	if err := json.Unmarshal([]byte(toolResult.Output), &output); err != nil {
		t.Fatalf("SemanticSearch output is not valid JSON: %v", err)
	}
	if output.Status != "ready" || !output.Index.Ready {
		t.Fatalf("semantic_search readiness = %+v, want ready", output.Index)
	}
	if output.Index.SnapshotID == nil || *output.Index.SnapshotID != workflow.snapshotID {
		t.Fatalf("snapshot_id = %+v, want %d", output.Index.SnapshotID, workflow.snapshotID)
	}
	if output.Query != query {
		t.Fatalf("query = %q, want %q", output.Query, query)
	}
	if output.TopK != 3 {
		t.Fatalf("top_k = %d, want 3", output.TopK)
	}
	if len(output.Results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(output.Results))
	}

	wantPaths := []string{
		"docs/semantic-retrieval-guide.md",
		"internal/runtime/retrieval_prompting.md",
		"internal/indexing/status_notes.md",
	}
	for i, result := range output.Results {
		if result.Rank != i+1 {
			t.Fatalf("result[%d].rank = %d, want %d", i, result.Rank, i+1)
		}
		if result.FilePath != wantPaths[i] {
			t.Fatalf("result[%d].file_path = %q, want %q", i, result.FilePath, wantPaths[i])
		}
		if result.ChunkID == "" || !strings.HasPrefix(result.ChunkID, result.FilePath+"#") {
			t.Fatalf("result[%d].chunk_id = %q, want %q#...", i, result.ChunkID, result.FilePath)
		}
		if result.Score <= 0 {
			t.Fatalf("result[%d].score = %v, want positive score", i, result.Score)
		}
		if strings.TrimSpace(result.Snippet) == "" {
			t.Fatalf("result[%d].snippet is empty", i)
		}
		if i > 0 && result.Score > output.Results[i-1].Score {
			t.Fatalf("result[%d].score = %v, want descending order after %v", i, result.Score, output.Results[i-1].Score)
		}
	}

	provider := &captureProvider{
		responses: []runtime.ProviderResponse{{
			AssistantMessage: runtime.Message{
				Role:    runtime.MessageRoleAssistant,
				Content: "Here is the retrieval evidence.",
			},
			StopReason: runtime.StopReasonComplete,
		}},
	}
	orchestrator := &runtime.Orchestrator{
		Provider:          provider,
		SemanticRetriever: retriever,
	}

	_, err = orchestrator.RunTurn(ctx, runtime.TurnInput{
		SessionID:   bootstrap.Session.ThreadID,
		UserMessage: query,
		Config: runtime.ExecutionConfig{
			MaxToolIterations: 1,
		},
	})
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	if len(provider.requests[0].Conversation) != 2 {
		t.Fatalf("provider conversation len = %d, want 2", len(provider.requests[0].Conversation))
	}

	retrievalContext := provider.requests[0].Conversation[0]
	if retrievalContext.Role != runtime.MessageRoleSystem {
		t.Fatalf("retrieval message role = %q, want %q", retrievalContext.Role, runtime.MessageRoleSystem)
	}
	if !strings.Contains(retrievalContext.Content, "Semantic retrieval evidence for the current repository:") {
		t.Fatalf("retrieval context missing header: %q", retrievalContext.Content)
	}
	if !strings.Contains(retrievalContext.Content, "Index status: ready.") {
		t.Fatalf("retrieval context missing ready status: %q", retrievalContext.Content)
	}
	internalResults := parseInternalRetrievalResults(t, retrievalContext.Content)
	if len(internalResults) == 0 {
		t.Fatalf("internal retrieval results = %d, want non-empty ranked context", len(internalResults))
	}
	for i, result := range internalResults {
		if result.Rank != i+1 {
			t.Fatalf("internal result[%d].rank = %d, want %d", i, result.Rank, i+1)
		}
		if result.FilePath == "" || result.ChunkID == "" || result.Score <= 0 {
			t.Fatalf("internal result[%d] = %+v, want populated evidence", i, result)
		}
	}
	if internalResults[0].FilePath != output.Results[0].FilePath {
		t.Fatalf("internal top file_path = %q, want %q", internalResults[0].FilePath, output.Results[0].FilePath)
	}
}

type semanticSearchOutput struct {
	Status  string                           `json:"status"`
	Query   string                           `json:"query"`
	TopK    int                              `json:"top_k"`
	Index   retrieval.SemanticIndexReadiness `json:"index"`
	Results []retrieval.SemanticQueryResult  `json:"results"`
}

type fixtureSemanticRetriever struct {
	t         *testing.T
	workflow  *fakeIndexWorkflow
	sessionID string
	repoRoot  string
	fixtures  []fixtureDocument
}

func newFixtureSemanticRetriever(t *testing.T, workflow *fakeIndexWorkflow, sessionID, repoRoot, fixtureRoot string) *fixtureSemanticRetriever {
	t.Helper()

	fixtures := []fixtureDocument{
		loadFixtureDocument(t, fixtureRoot, "docs/semantic-retrieval-guide.md", 0),
		loadFixtureDocument(t, fixtureRoot, "internal/runtime/retrieval_prompting.md", 1),
		loadFixtureDocument(t, fixtureRoot, "internal/indexing/status_notes.md", 2),
	}
	return &fixtureSemanticRetriever{
		t:         t,
		workflow:  workflow,
		sessionID: sessionID,
		repoRoot:  repoRoot,
		fixtures:  fixtures,
	}
}

func (r *fixtureSemanticRetriever) Query(_ context.Context, req retrieval.SemanticQueryRequest) (retrieval.SemanticQueryResponse, error) {
	r.t.Helper()

	if req.SessionID != r.sessionID {
		return retrieval.SemanticQueryResponse{}, fmt.Errorf("session_id = %q, want %q", req.SessionID, r.sessionID)
	}
	if req.RepoRoot != r.repoRoot {
		return retrieval.SemanticQueryResponse{}, fmt.Errorf("repo_root = %q, want %q", req.RepoRoot, r.repoRoot)
	}

	resp := retrieval.SemanticQueryResponse{
		SessionID: req.SessionID,
		RepoRoot:  req.RepoRoot,
		Query:     req.Query,
		TopK:      req.TopK,
		Index:     r.workflow.readiness(),
	}
	if !resp.Index.Ready {
		return resp, nil
	}

	results := rankFixtureDocuments(strings.TrimSpace(req.Query), r.fixtures)
	if req.TopK > 0 && len(results) > req.TopK {
		results = results[:req.TopK]
	}
	resp.Results = retrieval.ApplyStableRanks(results)
	return resp, nil
}

type fixtureDocument struct {
	path       string
	chunkIndex int
	content    string
	snippet    string
}

func loadFixtureDocument(t *testing.T, repoRoot, relativePath string, chunkIndex int) fixtureDocument {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(repoRoot, relativePath))
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", relativePath, err)
	}
	content := strings.TrimSpace(string(raw))
	snippet := content
	if newline := strings.IndexByte(snippet, '\n'); newline >= 0 {
		snippet = snippet[:newline]
	}
	return fixtureDocument{
		path:       relativePath,
		chunkIndex: chunkIndex,
		content:    content,
		snippet:    snippet,
	}
}

func rankFixtureDocuments(query string, fixtures []fixtureDocument) []retrieval.SemanticQueryResult {
	tokens := semanticTokens(query)
	results := make([]retrieval.SemanticQueryResult, 0, len(fixtures))
	for _, fixture := range fixtures {
		matchCount := semanticMatchCount(tokens, fixture.path+" "+fixture.content)
		if matchCount == 0 {
			continue
		}

		score := float64(matchCount) / float64(len(tokens))
		results = append(results, retrieval.SemanticQueryResult{
			FilePath: fixture.path,
			ChunkID:  fmt.Sprintf("%s#%d", fixture.path, fixture.chunkIndex),
			Score:    score,
			Snippet:  retrieval.BoundSnippet(fixture.snippet),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].FilePath != results[j].FilePath {
			return results[i].FilePath < results[j].FilePath
		}
		return results[i].ChunkID < results[j].ChunkID
	})
	return results
}

func semanticTokens(input string) []string {
	fields := strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})

	seen := make(map[string]struct{}, len(fields))
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		tokens = append(tokens, field)
	}
	return tokens
}

func semanticMatchCount(tokens []string, haystack string) int {
	lower := strings.ToLower(haystack)
	count := 0
	for _, token := range tokens {
		if strings.Contains(lower, token) {
			count++
		}
	}
	return count
}

type fakeIndexWorkflow struct {
	sessionID   string
	repoRoot    string
	stage       int
	snapshotID  int64
	completedAt time.Time
}

func newFakeIndexWorkflow(sessionID, repoRoot string) *fakeIndexWorkflow {
	return &fakeIndexWorkflow{
		sessionID:   sessionID,
		repoRoot:    repoRoot,
		snapshotID:  91,
		completedAt: time.Date(2026, time.March, 18, 15, 6, 0, 0, time.UTC),
	}
}

func (w *fakeIndexWorkflow) Advance() {
	if w.stage < 2 {
		w.stage++
	}
}

func (w *fakeIndexWorkflow) readiness() retrieval.SemanticIndexReadiness {
	if w.stage < 2 {
		return retrieval.SemanticIndexReadiness{
			Ready:  false,
			Status: "index_not_ready",
		}
	}
	return retrieval.SemanticIndexReadiness{
		Ready:      true,
		Status:     "ready",
		SnapshotID: &w.snapshotID,
		UpdatedAt:  &w.completedAt,
	}
}

func (w *fakeIndexWorkflow) LoadLatestSnapshot(_ context.Context, sessionID, repoRoot string) (*indexstore.SnapshotState, error) {
	if sessionID != w.sessionID || repoRoot != w.repoRoot {
		return nil, fmt.Errorf("unexpected snapshot scope: %q %q", sessionID, repoRoot)
	}
	if w.stage < 2 {
		return nil, nil
	}
	return &indexstore.SnapshotState{
		Root: indexsync.SnapshotRoot{
			ID:          w.snapshotID,
			SessionID:   w.sessionID,
			RepoRoot:    w.repoRoot,
			RootHash:    "root-semantic-slice",
			Status:      indexsync.SnapshotStatusActive,
			IsActive:    true,
			CompletedAt: &w.completedAt,
		},
	}, nil
}

func (w *fakeIndexWorkflow) ListJobs(_ context.Context, sessionID, repoRoot string) ([]indexstatus.JobRecord, error) {
	if sessionID != w.sessionID || repoRoot != w.repoRoot {
		return nil, fmt.Errorf("unexpected job scope: %q %q", sessionID, repoRoot)
	}

	enqueuedAt := w.completedAt.Add(-2 * time.Minute)
	startedAt := w.completedAt.Add(-90 * time.Second)
	finishedAt := w.completedAt
	switch w.stage {
	case 0:
		return []indexstatus.JobRecord{{
			JobID:      701,
			Kind:       indexstatus.JobKindSync,
			State:      indexstatus.JobStatePending,
			Attempt:    1,
			EnqueuedAt: &enqueuedAt,
		}}, nil
	case 1:
		return []indexstatus.JobRecord{
			{
				JobID:      701,
				Kind:       indexstatus.JobKindSync,
				State:      indexstatus.JobStateRunning,
				Attempt:    1,
				EnqueuedAt: &enqueuedAt,
				StartedAt:  &startedAt,
			},
			{
				JobID:      702,
				Kind:       indexstatus.JobKindIndex,
				State:      indexstatus.JobStatePending,
				Attempt:    1,
				EnqueuedAt: &startedAt,
			},
		}, nil
	default:
		return []indexstatus.JobRecord{
			{
				JobID:      701,
				Kind:       indexstatus.JobKindSync,
				State:      indexstatus.JobStateSucceeded,
				Attempt:    1,
				EnqueuedAt: &enqueuedAt,
				StartedAt:  &startedAt,
				FinishedAt: &finishedAt,
				SnapshotID: &w.snapshotID,
				RootHash:   "root-semantic-slice",
			},
			{
				JobID:      702,
				Kind:       indexstatus.JobKindIndex,
				State:      indexstatus.JobStateSucceeded,
				Attempt:    1,
				EnqueuedAt: &startedAt,
				StartedAt:  &startedAt,
				FinishedAt: &finishedAt,
				SnapshotID: &w.snapshotID,
				RootHash:   "root-semantic-slice",
				DeltaSize:  3,
			},
		}, nil
	}
}

func waitForIndexReady(t *testing.T, service *indexstatus.Service, workflow *fakeIndexWorkflow, sessionID, repoRoot string) {
	t.Helper()

	for attempt := 0; attempt < 3; attempt++ {
		status, err := service.GetIndexSyncStatus(context.Background(), sessionID, repoRoot)
		if err != nil {
			t.Fatalf("GetIndexSyncStatus returned error: %v", err)
		}
		if status.LatestSnapshot.Status == string(indexsync.SnapshotStatusActive) {
			if status.LastSuccessfulSyncAt == nil || status.LastSuccessfulIndexAt == nil {
				t.Fatalf("ready status missing completion timestamps: %+v", status)
			}
			return
		}
		workflow.Advance()
	}

	t.Fatal("index workflow did not become ready")
}

type captureProvider struct {
	responses []runtime.ProviderResponse
	requests  []runtime.ProviderRequest
}

func (p *captureProvider) CompleteTurn(_ context.Context, request runtime.ProviderRequest) (runtime.ProviderResponse, error) {
	p.requests = append(p.requests, request)
	if len(p.responses) == 0 {
		return runtime.ProviderResponse{}, fmt.Errorf("no provider response queued")
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}

type stubSessionStore struct {
	sessionID       string
	now             time.Time
	createdRepoRoot string
}

func (s *stubSessionStore) CreateSession(_ context.Context, params session.CreateSessionParams) (session.Session, error) {
	s.createdRepoRoot = params.RepoRoot
	return session.Session{
		ThreadID:  s.sessionID,
		RepoRoot:  params.RepoRoot,
		CreatedAt: s.now,
	}, nil
}

func (*stubSessionStore) ResumeSession(context.Context, string) (session.Session, error) {
	return session.Session{}, fmt.Errorf("resume session should not be called in create flow")
}

func (*stubSessionStore) AppendMessage(context.Context, session.AppendMessageParams) (session.Message, error) {
	return session.Message{}, fmt.Errorf("append message is not used in this test")
}

func (*stubSessionStore) ListMessages(context.Context, string) ([]session.Message, error) {
	return nil, fmt.Errorf("list messages is not used in this test")
}

func restoreSemanticSearchFactories(t *testing.T) {
	t.Helper()

	originalPool := linkedNewSemanticSearchPool
	originalRetriever := linkedNewSemanticRetriever
	originalClose := linkedCloseSemanticSearchPool
	t.Cleanup(func() {
		linkedNewSemanticSearchPool = originalPool
		linkedNewSemanticRetriever = originalRetriever
		linkedCloseSemanticSearchPool = originalClose
	})
}

func bindSessionScope(t *testing.T, sessionID, repoRoot string) context.Context {
	t.Helper()

	ctx, err := runtime.WithRepoRoot(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("WithRepoRoot returned error: %v", err)
	}
	return runtime.WithSessionID(ctx, sessionID)
}

func semanticSearchToolCall(t *testing.T, input handlers.SemanticSearchInput) runtime.ToolCall {
	t.Helper()

	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	return runtime.ToolCall{
		ID:        "call-semantic-slice",
		Name:      "semantic_search",
		Arguments: raw,
	}
}

func parseInternalRetrievalResults(t *testing.T, retrievalContext string) []retrieval.SemanticQueryResult {
	t.Helper()

	lines := strings.Split(retrievalContext, "\n")
	results := make([]retrieval.SemanticQueryResult, 0)
	for _, line := range lines {
		matches := retrievalLinePattern.FindStringSubmatch(strings.TrimSpace(line))
		if matches == nil {
			continue
		}

		var rank int
		if _, err := fmt.Sscanf(matches[1], "%d", &rank); err != nil {
			t.Fatalf("parse rank %q: %v", matches[1], err)
		}
		var score float64
		if _, err := fmt.Sscanf(matches[4], "%f", &score); err != nil {
			t.Fatalf("parse score %q: %v", matches[4], err)
		}

		results = append(results, retrieval.SemanticQueryResult{
			Rank:     rank,
			FilePath: matches[2],
			ChunkID:  matches[3],
			Score:    score,
		})
	}
	return results
}

func seedSemanticFixtureRepo(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	writeFixtureFile(t, repoRoot, "docs/semantic-retrieval-guide.md", strings.Join([]string{
		"Semantic retrieval evidence is documented here for operators.",
		"Use semantic_search to inspect ranked evidence with file_path, score, chunk_id, and snippet fields.",
	}, "\n"))
	writeFixtureFile(t, repoRoot, "internal/runtime/retrieval_prompting.md", strings.Join([]string{
		"Runtime retrieval injects ranked evidence into the prompt before the final user message.",
		"The retrieval hook formats transparent ranked chunks so the model can cite repository context safely.",
	}, "\n"))
	writeFixtureFile(t, repoRoot, "internal/indexing/status_notes.md", strings.Join([]string{
		"Index sync becomes ready after the background snapshot reaches active status.",
		"Operators can poll status until the repository is ready for semantic search queries.",
	}, "\n"))
	return repoRoot
}

func writeFixtureFile(t *testing.T, repoRoot, relativePath, content string) {
	t.Helper()

	fullPath := filepath.Join(repoRoot, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error: %v", filepath.Dir(relativePath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", relativePath, err)
	}
}
