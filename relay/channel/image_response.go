package channel

import (
	"errors"
	"io"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// WriteImageResponse serializes potentially large base64 images directly to
// the private response spool. It never builds a second response-sized []byte.
func WriteImageResponse(writer io.Writer, response *dto.ImageResponse) error {
	if writer == nil || response == nil {
		return errors.New("image response writer or payload is nil")
	}
	if _, err := io.WriteString(writer, `{"data":[`); err != nil {
		return err
	}
	for index, image := range response.Data {
		if index > 0 {
			if _, err := io.WriteString(writer, ","); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(writer, `{"url":`); err != nil {
			return err
		}
		if err := common.WriteJSONString(writer, image.Url); err != nil {
			return err
		}
		if _, err := io.WriteString(writer, `,"b64_json":`); err != nil {
			return err
		}
		if err := common.WriteJSONString(writer, image.B64Json); err != nil {
			return err
		}
		if _, err := io.WriteString(writer, `,"revised_prompt":`); err != nil {
			return err
		}
		if err := common.WriteJSONString(writer, image.RevisedPrompt); err != nil {
			return err
		}
		if _, err := io.WriteString(writer, "}"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(writer, `],"created":`+strconv.FormatInt(response.Created, 10)); err != nil {
		return err
	}
	if len(response.Metadata) > 0 {
		if _, err := io.WriteString(writer, `,"metadata":`); err != nil {
			return err
		}
		if _, err := writer.Write(response.Metadata); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "}")
	return err
}
