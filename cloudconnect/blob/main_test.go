package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

type fakeBlob struct {
	container, name, body string
}

func (f *fakeBlob) UploadFile(_ context.Context, container, name string, file *os.File, _ *azblob.UploadFileOptions) (azblob.UploadFileResponse, error) {
	f.container, f.name = container, name
	b, _ := io.ReadAll(file)
	f.body = string(b)
	return azblob.UploadFileResponse{}, nil
}

func TestUploadMapping(t *testing.T) {
	p := filepath.Join(t.TempDir(), "1.csv")
	_ = os.WriteFile(p, []byte("d1,pump\n"), 0o644)
	f, _ := os.Open(p)
	defer f.Close()
	fake := &fakeBlob{}
	if err := uploader(context.Background(), fake, "cont")(f, "d1/1.csv"); err != nil {
		t.Fatal(err)
	}
	if fake.container != "cont" || fake.name != "d1/1.csv" || fake.body != "d1,pump\n" {
		t.Errorf("upload = %+v", fake)
	}
}
