package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

const imageResponseJSONOverheadPerImage = int64(128)

var ErrImageResponseBudgetExceeded = errors.New("image response encoded budget exceeded")

// ImageResponseEncodedBudget bounds the cumulative base64 material retained
// while provider image URLs are converted into one client JSON response. The
// budget is no larger than either the ordinary upstream JSON limit or the
// private response spool limit.
type ImageResponseEncodedBudget struct {
	limit int64
	used  int64
}

func NewImageResponseEncodedBudget() *ImageResponseEncodedBudget {
	limit := common.UpstreamJSONBodyLimit()
	if spoolLimit := common.BufferedResponseBodyLimit(); spoolLimit < limit {
		limit = spoolLimit
	}
	return &ImageResponseEncodedBudget{limit: limit}
}

// ReserveEncodedBytes accounts for already-retained response fields, such as
// provider metadata copied into the client response, before URL images are
// downloaded and base64-expanded.
func (b *ImageResponseEncodedBudget) ReserveEncodedBytes(size int64) error {
	if b == nil {
		return errors.New("image response encoded budget is unavailable")
	}
	if size < 0 {
		return errors.New("image response encoded byte reservation is invalid")
	}
	if size > b.limit-b.used {
		return fmt.Errorf("%w: limit=%d used=%d attempted=%d", ErrImageResponseBudgetExceeded, b.limit, b.used, size)
	}
	b.used += size
	return nil
}

func (b *ImageResponseEncodedBudget) ConsumeBase64(data string) error {
	if b == nil {
		return errors.New("image response encoded budget is unavailable")
	}
	if data == "" {
		return errors.New("image response base64 data is empty")
	}
	charge := int64(len(data)) + imageResponseJSONOverheadPerImage
	if charge > b.limit-b.used {
		return fmt.Errorf("%w: limit=%d used=%d attempted=%d", ErrImageResponseBudgetExceeded, b.limit, b.used, charge)
	}
	b.used += charge
	return nil
}

// RemainingRawBytes returns a conservative raw-byte download ceiling whose
// padded standard-base64 encoding plus JSON overhead fits in the budget.
func (b *ImageResponseEncodedBudget) RemainingRawBytes() int64 {
	if b == nil {
		return 0
	}
	remaining := b.limit - b.used - imageResponseJSONOverheadPerImage
	if remaining < 4 {
		return 0
	}
	return remaining / 4 * 3
}

func (b *ImageResponseEncodedBudget) Limit() int64 {
	if b == nil {
		return 0
	}
	return b.limit
}
