package ssh

import "context"

// MockExecutor implements SSHExecutor for unit tests.
// Each method returns its corresponding Result/Err fields.
// Place in the ssh package (mock_test.go) so it is available to internal/ssh tests;
// tools package tests define their own mock against the SSHExecutor interface.
type MockExecutor struct {
	RunResult      RunResult
	RunErr         error
	ReadContent    []byte
	ReadErr        error
	EncodingResult string
	EncodingErr    error
	WriteErr       error
	ListResult     string
	ListErr        error
	UploadErr      error
	DownloadErr    error
	CheckErr       error
}

func (m *MockExecutor) RunCommand(_ context.Context, _ RunRequest) (RunResult, error) {
	return m.RunResult, m.RunErr
}

func (m *MockExecutor) ReadFile(_ context.Context, _ string) ([]byte, error) {
	return m.ReadContent, m.ReadErr
}

func (m *MockExecutor) DetectEncoding(_ context.Context, _ string) (string, error) {
	return m.EncodingResult, m.EncodingErr
}

func (m *MockExecutor) WriteFile(_ context.Context, _ string, _ []byte) error {
	return m.WriteErr
}

func (m *MockExecutor) ListDir(_ context.Context, _ string) (string, error) {
	return m.ListResult, m.ListErr
}

func (m *MockExecutor) UploadFile(_ context.Context, _, _ string) error {
	return m.UploadErr
}

func (m *MockExecutor) DownloadFile(_ context.Context, _, _ string) error {
	return m.DownloadErr
}

func (m *MockExecutor) CheckSocket(_ context.Context) error {
	return m.CheckErr
}
