package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

const csvHeader = "Α/Α;Ημερομηνία;Αιτιολογία;Κατάστημα;Τοκισμός από;Αρ. συναλλαγής;Ποσό;Πρόσημο ποσού;"

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

func TestHandleIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	New(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `action="/generate"`)
	require.Contains(t, rec.Body.String(), `name="files"`)
}

func TestHandleGenerate(t *testing.T) {
	t.Run("generates a report from an uploaded csv", func(t *testing.T) {
		body := csvHeader + "\n" +
			`1;29/05/2026;="SHOP";9;27/5/2026;="ID1";53,79;Χ;` + "\n" +
			`2;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;` + "\n"
		buf, ct := multipartCSV(t, "acc.csv", body)

		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(nil).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), "May 2026")
		require.Contains(t, rec.Body.String(), "Generate another")
	})

	t.Run("rejects when no files are uploaded", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		require.NoError(t, w.Close())

		req := httptest.NewRequest(http.MethodPost, "/generate", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		rec := httptest.NewRecorder()
		New(nil).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "at least one CSV")
	})

	t.Run("rejects a malformed multipart body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader([]byte("garbage")))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=nope")
		rec := httptest.NewRecorder()
		New(nil).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestBrowserURL(t *testing.T) {
	require.Equal(t, "http://localhost:8080", browserURL(":8080"))
	require.Equal(t, "http://127.0.0.1:9000", browserURL("127.0.0.1:9000"))
}

func TestWithBackLink(t *testing.T) {
	require.Contains(t, string(withBackLink([]byte("<html><body>x</body></html>"))), "Generate another")
	require.Equal(t, "no-body", string(withBackLink([]byte("no-body"))))
}
