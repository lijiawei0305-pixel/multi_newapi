package service

import "errors"

type explicitTaskSubmissionRejection struct {
	err error
}

func (e *explicitTaskSubmissionRejection) Error() string {
	return e.err.Error()
}

func (e *explicitTaskSubmissionRejection) Unwrap() error {
	return e.err
}

// ExplicitTaskSubmissionRejection marks a parsed provider response that
// affirmatively says the task was not accepted. Transport and parse errors
// must never use this marker.
func ExplicitTaskSubmissionRejection(err error) error {
	if err == nil {
		return nil
	}
	return &explicitTaskSubmissionRejection{err: err}
}

func IsExplicitTaskSubmissionRejection(err error) bool {
	var rejection *explicitTaskSubmissionRejection
	return errors.As(err, &rejection)
}
