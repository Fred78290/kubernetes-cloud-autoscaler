package api

import "fmt"

type ApiError interface {
	// Error implements golang error interface
	Error() string
}

type apiErrorImpl struct {
	code   int32
	reason string
}

func NewApiError(err *ClientError) ApiError {
	return &apiErrorImpl{
		code:   err.GetCode(),
		reason: err.GetReason(),
	}
}

func (e *apiErrorImpl) Error() string {
	return fmt.Sprintf("client: code=%d, reason=%s", e.code, e.reason)
}
