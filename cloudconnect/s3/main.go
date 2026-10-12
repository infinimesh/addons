package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/infinimesh/addons/cloudconnect/csvprocessor"
)

const envS3BucketName = "AWS_S3BUCKETNAME"

func main() {
	ctx := context.Background()
	bucketName := os.Getenv(envS3BucketName)
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("failed to load AWS config: %s", err)
	}
	svc := s3.NewFromConfig(cfg)
	// The bucket may already exist, so an error here is not fatal.
	_, _ = svc.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucketName)})
	csvprocessor.WalkLoop(csvprocessor.WalkFunc(func(f *os.File) error {
		_, err := svc.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(filepath.Base(f.Name())),
			Body:   f,
		})
		return err
	}))
}
