package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/infinimesh/addons/cloudconnect/csvprocessor"
)

const (
	envAzAccountName   = "AZ_ACCOUNT_NAME"
	envAzAccountKey    = "AZ_ACCOUNT_KEY"
	envAzContainerName = "AZ_CONTAINER_NAME"
)

func main() {
	accountName := os.Getenv(envAzAccountName)
	containerName := os.Getenv(envAzContainerName)
	credential, err := azblob.NewSharedKeyCredential(accountName, os.Getenv(envAzAccountKey))
	if err != nil {
		log.Fatal(err)
	}
	client, err := azblob.NewClientWithSharedKeyCredential(fmt.Sprintf("https://%s.blob.core.windows.net/", accountName), credential, nil)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	// The container may already exist, so an error here is not fatal.
	_, _ = client.CreateContainer(ctx, containerName, nil)
	csvprocessor.WalkLoop(uploader(ctx, client, containerName))
}

type fileUploader interface {
	UploadFile(ctx context.Context, containerName, blobName string, file *os.File, o *azblob.UploadFileOptions) (azblob.UploadFileResponse, error)
}

// uploader uploads each spool file to the container under its spool name.
func uploader(ctx context.Context, client fileUploader, containerName string) func(f *os.File, name string) error {
	return func(f *os.File, name string) error {
		_, err := client.UploadFile(ctx, containerName, name, f, nil)
		return err
	}
}
