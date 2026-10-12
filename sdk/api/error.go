package api

import "fmt"

func NewFailedRequestError(method string, err error) error {
	return fmt.Errorf("infinimesh API call %s failed: %w", method, err)
}
