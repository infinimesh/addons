package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type fakeS3 struct {
	bucket, key, body string
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.bucket, f.key = *in.Bucket, *in.Key
	b, _ := io.ReadAll(in.Body)
	f.body = string(b)
	return &s3.PutObjectOutput{}, nil
}

func TestUploadMapping(t *testing.T) {
	p := filepath.Join(t.TempDir(), "1.csv")
	_ = os.WriteFile(p, []byte("d1,pump\n"), 0o644)
	f, _ := os.Open(p)
	defer f.Close()
	fake := &fakeS3{}
	if err := uploader(context.Background(), fake, "bkt")(f, "d1/1.csv"); err != nil {
		t.Fatal(err)
	}
	if fake.bucket != "bkt" || fake.key != "d1/1.csv" || fake.body != "d1,pump\n" {
		t.Errorf("put = %+v", fake)
	}
}
