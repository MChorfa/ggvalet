package plan

import (
	"regexp"
	"strings"
	"testing"
)

func TestMilestoneIdentity_Deterministic(t *testing.T) {
	m := Milestone{
		ID:      "m-1",
		Title:   "Q4 2026 Week 7",
		DueDate: "2026-10-14",
		State:   "active",
	}

	h1 := MilestoneIdentity(m)
	h2 := MilestoneIdentity(m)

	if h1 != h2 {
		t.Errorf("MilestoneIdentity not deterministic: got %s, then %s", h1, h2)
	}
}

func TestMilestoneIdentity_DueDateMatters(t *testing.T) {
	m1 := Milestone{
		ID:      "m-1",
		Title:   "Q4 Week 7",
		DueDate: "2026-10-14",
		State:   "active",
	}
	m2 := Milestone{
		ID:      "m-1",
		Title:   "Q4 Week 7",
		DueDate: "2026-10-15",
		State:   "active",
	}

	h1 := MilestoneIdentity(m1)
	h2 := MilestoneIdentity(m2)

	if h1 == h2 {
		t.Errorf("MilestoneIdentity should change with due_date: got same hash %s", h1)
	}
}

func TestMilestoneIdentity_64HexChars(t *testing.T) {
	m := Milestone{
		ID:      "m-1",
		Title:   "Week 7",
		DueDate: "2026-10-14",
		State:   "active",
	}

	h := MilestoneIdentity(m)

	if len(h) != 64 {
		t.Errorf("MilestoneIdentity length: got %d, want 64", len(h))
	}

	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(h) {
		t.Errorf("MilestoneIdentity not lowercase hex: got %s", h)
	}
}

func TestEpicIdentity_Deterministic(t *testing.T) {
	e := Epic{
		ID:    "e-1",
		Title: "Trusted Images",
	}

	h1 := EpicIdentity(e)
	h2 := EpicIdentity(e)

	if h1 != h2 {
		t.Errorf("EpicIdentity not deterministic: got %s, then %s", h1, h2)
	}
}

func TestIssueIdentity_TitleMatters(t *testing.T) {
	i1 := Issue{
		ID:        "i-1",
		Title:     "Implement foo",
		Milestone: "m-1",
		Epic:      "e-1",
	}
	i2 := Issue{
		ID:        "i-1",
		Title:     "Implement bar",
		Milestone: "m-1",
		Epic:      "e-1",
	}

	h1 := IssueIdentity(i1)
	h2 := IssueIdentity(i2)

	if h1 == h2 {
		t.Errorf("IssueIdentity should change with title: got same hash %s", h1)
	}
}

func TestIssueIdentity_MilestoneMatters(t *testing.T) {
	i1 := Issue{
		ID:        "i-1",
		Title:     "Implement foo",
		Milestone: "m-1",
		Epic:      "e-1",
	}
	i2 := Issue{
		ID:        "i-1",
		Title:     "Implement foo",
		Milestone: "m-2",
		Epic:      "e-1",
	}

	h1 := IssueIdentity(i1)
	h2 := IssueIdentity(i2)

	if h1 == h2 {
		t.Errorf("IssueIdentity should change with milestone: got same hash %s", h1)
	}
}

func TestIssueIdentity_EpicMatters(t *testing.T) {
	i1 := Issue{
		ID:        "i-1",
		Title:     "Implement foo",
		Milestone: "m-1",
		Epic:      "e-1",
	}
	i2 := Issue{
		ID:        "i-1",
		Title:     "Implement foo",
		Milestone: "m-1",
		Epic:      "e-2",
	}

	h1 := IssueIdentity(i1)
	h2 := IssueIdentity(i2)

	if h1 == h2 {
		t.Errorf("IssueIdentity should change with epic: got same hash %s", h1)
	}
}

func TestIssueIdentity_EmptyEpicValid(t *testing.T) {
	i := Issue{
		ID:        "i-1",
		Title:     "Implement foo",
		Milestone: "m-1",
		Epic:      "",
	}

	h := IssueIdentity(i)

	if len(h) != 64 {
		t.Errorf("IssueIdentity with empty epic length: got %d, want 64", len(h))
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(h) {
		t.Errorf("IssueIdentity with empty epic not lowercase hex: got %s", h)
	}
}

func TestEmbedMarker_AppendToEmpty(t *testing.T) {
	hash := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	result := EmbedMarker("", hash)
	expected := "<!-- ggvalet:plan-id=abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789 -->\n"

	if result != expected {
		t.Errorf("EmbedMarker to empty body: got %q, want %q", result, expected)
	}
}

func TestEmbedMarker_AppendToBody(t *testing.T) {
	hash := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	body := "Hello"
	result := EmbedMarker(body, hash)

	if !strings.HasPrefix(result, "Hello\n\n") {
		t.Errorf("EmbedMarker result should start with 'Hello\\n\\n': got %q", result)
	}
	if !strings.Contains(result, "<!-- ggvalet:plan-id=abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789 -->") {
		t.Errorf("EmbedMarker result should contain marker: got %q", result)
	}
}

func TestEmbedMarker_ReplacesExisting(t *testing.T) {
	oldHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	body := "Hello\n\n<!-- ggvalet:plan-id=" + oldHash + " -->\n"
	result := EmbedMarker(body, newHash)

	if strings.Contains(result, oldHash) {
		t.Errorf("EmbedMarker should replace old hash: got %q", result)
	}
	if !strings.Contains(result, newHash) {
		t.Errorf("EmbedMarker should contain new hash: got %q", result)
	}

	markerCount := len(regexp.MustCompile(`<!-- ggvalet:plan-id=`).FindAllString(result, -1))
	if markerCount != 1 {
		t.Errorf("EmbedMarker should have exactly 1 marker: got %d", markerCount)
	}
}

func TestExtractMarker_Present(t *testing.T) {
	hash := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	body := "Some description\n\n<!-- ggvalet:plan-id=" + hash + " -->\n"

	extracted, ok := ExtractMarker(body)

	if !ok {
		t.Fatalf("ExtractMarker should find marker, got ok=false")
	}
	if extracted != hash {
		t.Errorf("ExtractMarker: got %s, want %s", extracted, hash)
	}
}

func TestExtractMarker_Absent(t *testing.T) {
	body := "Some description without marker"

	extracted, ok := ExtractMarker(body)

	if ok {
		t.Errorf("ExtractMarker should not find marker: got ok=true, hash=%s", extracted)
	}
	if extracted != "" {
		t.Errorf("ExtractMarker with no marker should return empty string: got %s", extracted)
	}
}

func TestExtractMarker_RoundTrip(t *testing.T) {
	originalHash := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	body := "Original content"

	embedded := EmbedMarker(body, originalHash)
	extracted, ok := ExtractMarker(embedded)

	if !ok {
		t.Fatalf("ExtractMarker should find marker after EmbedMarker")
	}
	if extracted != originalHash {
		t.Errorf("Round-trip hash: got %s, want %s", extracted, originalHash)
	}
}

func TestExtractMarker_RejectsShortHash(t *testing.T) {
	body := "<!-- ggvalet:plan-id=abc -->"

	extracted, ok := ExtractMarker(body)

	if ok {
		t.Errorf("ExtractMarker should reject short hash: got ok=true, hash=%s", extracted)
	}
	if extracted != "" {
		t.Errorf("ExtractMarker with short hash should return empty string: got %s", extracted)
	}
}
