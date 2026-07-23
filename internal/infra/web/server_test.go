package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const csvHeader = "Α/Α;Ημερομηνία;Αιτιολογία;Κατάστημα;Τοκισμός από;Αρ. συναλλαγής;Ποσό;Πρόσημο ποσού;"

// tmpRules returns a fresh rules-file path inside a temp dir (seeded on first Load).
func tmpRules(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "rules.json")
}

// multipartCSV builds a multipart body with one CSV file under field "files".
func multipartCSV(t *testing.T, name, body string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("files", name)
	require.NoError(t, err)
	_, err = fw.Write([]byte(body))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return &buf, w.FormDataContentType()
}

// multipartWith builds a multipart body with one CSV file plus extra form fields
// (key may repeat for parallel rule columns).
func multipartWith(t *testing.T, name, body string, fields [][2]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("files", name)
	require.NoError(t, err)
	_, err = fw.Write([]byte(body))
	require.NoError(t, err)
	for _, kv := range fields {
		require.NoError(t, w.WriteField(kv[0], kv[1]))
	}
	require.NoError(t, w.Close())
	return &buf, w.FormDataContentType()
}

func TestHandleIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	New(tmpRules(t)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `action="/generate"`)
	require.Contains(t, rec.Body.String(), `name="files"`)
	// the seeded default rule is pre-filled
	require.Contains(t, rec.Body.String(), "SAMPLE DESCRIPTION")
}

func TestHandleGenerate(t *testing.T) {
	twoTxns := csvHeader + "\n" +
		`1;29/05/2026;="SHOP";9;27/5/2026;="ID1";53,79;Χ;` + "\n" +
		`2;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;` + "\n"

	t.Run("generates a report from an uploaded csv", func(t *testing.T) {
		buf, ct := multipartCSV(t, "acc.csv", twoTxns)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), "May 2026")
	})

	t.Run("applies a submitted exclusion rule", func(t *testing.T) {
		// Exclude the SHOP debit; only the SALARY credit should remain.
		fields := [][2]string{
			{"rule.matchMode", "exact"},
			{"rule.isDebit", "debit"},
			{"rule.description", "SHOP"},
			{"rule.sourceFile", ""},
		}
		buf, ct := multipartWith(t, "acc.csv", twoTxns, fields)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), "1.550")    // income kept
		require.NotContains(t, rec.Body.String(), "53,79") // SHOP expense excluded
	})

	t.Run("saves rules when the checkbox is set", func(t *testing.T) {
		path := tmpRules(t)
		fields := [][2]string{
			{"rule.matchMode", "contains"},
			{"rule.isDebit", "any"},
			{"rule.description", "SHOP"},
			{"rule.sourceFile", ""},
			{"save", "on"},
		}
		buf, ct := multipartWith(t, "acc.csv", twoTxns, fields)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(path).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Contains(t, string(data), `"description": "SHOP"`)
		require.Contains(t, string(data), `"matchMode": "contains"`)
	})

	t.Run("saves the VISA reconciliation config when the checkbox is set", func(t *testing.T) {
		path := tmpRules(t)
		fields := [][2]string{
			{"rule.matchMode", "exact"},
			{"rule.isDebit", "any"},
			{"rule.description", "SHOP"},
			{"rule.sourceFile", ""},
			{"visa.description", "ΠΛΗΡΩΜΗ VΙSΑ"},
			{"visa.matchMode", "exact"},
			{"visa.branch", "96"},
			{"save", "on"},
		}
		buf, ct := multipartWith(t, "acc.csv", twoTxns, fields)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(path).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Contains(t, string(data), `"visaReconcile"`)
		require.Contains(t, string(data), `"branch": "96"`)
	})

	t.Run("blank VISA description saves reconciliation off", func(t *testing.T) {
		path := tmpRules(t)
		fields := [][2]string{
			{"rule.matchMode", "exact"},
			{"rule.isDebit", "any"},
			{"rule.description", "SHOP"},
			{"rule.sourceFile", ""},
			{"visa.description", ""},
			{"visa.matchMode", "exact"},
			{"visa.branch", "96"},
			{"save", "on"},
		}
		buf, ct := multipartWith(t, "acc.csv", twoTxns, fields)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(path).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NotContains(t, string(data), `"visaReconcile"`)
	})

	t.Run("rejects an invalid VISA match mode", func(t *testing.T) {
		fields := [][2]string{
			{"visa.description", "ΠΛΗΡΩΜΗ VΙSΑ"},
			{"visa.matchMode", "fuzzy"},
			{"visa.branch", "96"},
		}
		buf, ct := multipartWith(t, "acc.csv", twoTxns, fields)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "VISA reconciliation")
	})

	t.Run("rejects an invalid rule row", func(t *testing.T) {
		fields := [][2]string{
			{"rule.matchMode", "exact"},
			{"rule.isDebit", "debit"},
			{"rule.description", ""},     // required
			{"rule.sourceFile", "x.csv"}, // makes the row non-blank
		}
		buf, ct := multipartWith(t, "acc.csv", twoTxns, fields)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "Rule 1")
	})

	t.Run("rejects when no files are uploaded", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		require.NoError(t, w.Close())

		req := httptest.NewRequest(http.MethodPost, "/generate", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "at least one CSV")
	})

	t.Run("rejects a malformed multipart body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader([]byte("garbage")))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=nope")
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestHandleIndexRendersVISASection(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	New(tmpRules(t)).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `name="visa.description"`)
	require.Contains(t, body, `name="visa.branch"`)
	require.Contains(t, body, `value="96"`) // seeded default branch
	// VISA section appears before the exclusion-rules editor.
	require.Less(t, strings.Index(body, `name="visa.description"`), strings.Index(body, `name="rule.description"`))
}

func TestParseRulesSkipsBlankRows(t *testing.T) {
	form := &multipart.Form{Value: map[string][]string{
		"rule.matchMode":   {"exact", "exact"},
		"rule.isDebit":     {"any", "debit"},
		"rule.description": {"", "RENT"},
		"rule.sourceFile":  {"", ""},
	}}
	specs, err := parseRules(form)
	require.NoError(t, err)
	require.Len(t, specs, 1)
	require.Equal(t, "RENT", specs[0].Description)
}

func TestBrowserURL(t *testing.T) {
	require.Equal(t, "http://localhost:8080", browserURL(":8080"))
	require.Equal(t, "http://127.0.0.1:9000", browserURL("127.0.0.1:9000"))
}
