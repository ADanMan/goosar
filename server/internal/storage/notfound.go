package storage

import (
	"errors"
	"io/fs"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func IsObjectNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}
	var notFound *types.NotFound
	return errors.As(err, &notFound)
}
