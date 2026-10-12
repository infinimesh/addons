package main

import (
	"context"
	"io"
	"log"
	"os"

	"github.com/infinimesh/addons/cloudconnect/csvprocessor"

	"cloud.google.com/go/storage"
)

const (
	envGcpProjectID  = "GCP_PROJECT_ID"
	envGcpBucketName = "GCP_BUCKET_NAME"
)

func main() {
	bucketName := os.Getenv(envGcpBucketName)
	ctx := context.Background()
	c, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}
	_ = c.Bucket(bucketName).Create(ctx, os.Getenv(envGcpProjectID), nil)
	csvprocessor.WalkLoop(func(f *os.File, name string) error {
		w := c.Bucket(bucketName).Object(name).NewWriter(ctx)
		if _, err := io.Copy(w, f); err != nil {
			_ = w.Close()
			return err
		}
		// the upload only finishes on Close, so its error decides if the file is deleted
		return w.Close()
	})
}
