package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"golang.org/x/image/webp"
)

// return image.Config, format, clean base64 string, error
func DecodeBase64ImageData(base64String string) (image.Config, string, string, error) {
	// 去除base64数据的URL前缀（如果有）
	if idx := strings.Index(base64String, ","); idx != -1 {
		base64String = base64String[idx+1:]
	}

	if len(base64String) == 0 {
		return image.Config{}, "", "", errors.New("base64 string is empty")
	}

	// 将base64字符串解码为字节切片
	decodedData, err := base64.StdEncoding.DecodeString(base64String)
	if err != nil {
		fmt.Println("Error: Failed to decode base64 string")
		return image.Config{}, "", "", fmt.Errorf("failed to decode base64 string: %s", err.Error())
	}

	config, format, err := getImageConfig(decodedData)
	return config, format, base64String, err
}

func DecodeBase64FileData(base64String string) (string, string, error) {
	var mimeType string
	var idx int
	idx = strings.Index(base64String, ",")
	if idx == -1 {
		_, file_type, base64, err := DecodeBase64ImageData(base64String)
		return "image/" + file_type, base64, err
	}
	mimeType = base64String[:idx]
	base64String = base64String[idx+1:]
	idx = strings.Index(mimeType, ";")
	if idx == -1 {
		_, file_type, base64, err := DecodeBase64ImageData(base64String)
		return "image/" + file_type, base64, err
	}
	mimeType = mimeType[:idx]
	idx = strings.Index(mimeType, ":")
	if idx == -1 {
		_, file_type, base64, err := DecodeBase64ImageData(base64String)
		return "image/" + file_type, base64, err
	}
	mimeType = mimeType[idx+1:]
	return mimeType, base64String, nil
}

// GetImageFromUrl 获取图片的类型和base64编码的数据
func GetImageFromUrl(url string) (mimeType string, data string, err error) {
	return GetImageFromURLContext(context.Background(), url)
}

func GetImageFromURLContext(ctx context.Context, url string) (mimeType string, data string, err error) {
	maxImageSize := int64(constant.MaxFileDownloadMB) * 1024 * 1024
	return GetImageFromURLContextWithLimit(ctx, url, maxImageSize)
}

// GetImageFromURLContextWithLimit downloads one image with a caller-provided
// raw-byte ceiling. Media response adaptors use this to keep cumulative base64
// conversion within their private response budget.
func GetImageFromURLContextWithLimit(ctx context.Context, url string, maxImageSize int64) (mimeType string, data string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	configuredMaxImageSize := int64(constant.MaxFileDownloadMB) * 1024 * 1024
	if configuredMaxImageSize > 0 && maxImageSize > configuredMaxImageSize {
		maxImageSize = configuredMaxImageSize
	}
	if maxImageSize <= 0 {
		return "", "", errors.New("image download byte limit is exhausted")
	}
	timeoutSeconds := common.GetEnvOrDefault("RELAY_IMAGE_DOWNLOAD_TIMEOUT_SECONDS", 30)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	resp, err := DoDownloadRequestContext(ctx, url)
	if err != nil {
		return "", "", fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("failed to download image: HTTP %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/octet-stream" && !strings.HasPrefix(contentType, "image/") {
		return "", "", fmt.Errorf("invalid content type: %s, required image/*", contentType)
	}
	// Check Content-Length if available
	if resp.ContentLength > maxImageSize {
		return "", "", fmt.Errorf("image size %d exceeds maximum allowed size of %d bytes", resp.ContentLength, maxImageSize)
	}

	imageData, err := common.ReadAllWithLimit(resp.Body, maxImageSize)
	if err != nil {
		return "", "", fmt.Errorf("failed to read image data: %w", err)
	}

	data = base64.StdEncoding.EncodeToString(imageData)
	mimeType = contentType

	// Handle application/octet-stream type
	if mimeType == "application/octet-stream" {
		_, format, _, err := DecodeBase64ImageData(data)
		if err != nil {
			return "", "", err
		}
		mimeType = "image/" + format
	}

	return mimeType, data, nil
}

func DecodeUrlImageData(imageUrl string) (image.Config, string, error) {
	return DecodeURLImageDataContext(context.Background(), imageUrl)
}

func DecodeURLImageDataContext(ctx context.Context, imageUrl string) (image.Config, string, error) {
	ctx, cancel := boundedFileDownloadContext(ctx)
	defer cancel()
	response, err := DoDownloadRequestContext(ctx, imageUrl)
	if err != nil {
		common.SysLog(fmt.Sprintf("fail to get image from url_%s error_type=%T", common.PayloadMetadata([]byte(imageUrl)), err))
		return image.Config{}, "", err
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		err = fmt.Errorf("fail to get image from url: %s", response.Status)
		return image.Config{}, "", err
	}

	mimeType := response.Header.Get("Content-Type")

	if mimeType != "application/octet-stream" && !strings.HasPrefix(mimeType, "image/") {
		return image.Config{}, "", fmt.Errorf("invalid content type: %s, required image/*", mimeType)
	}

	var readData []byte
	var decodeErr error
	for _, limit := range []int{8 << 10, 24 << 10, 64 << 10} {
		common.SysLog(fmt.Sprintf("try to decode image config with limit: %d", limit))

		additionalData := make([]byte, limit-len(readData))
		n, readErr := io.ReadFull(response.Body, additionalData)
		readData = append(readData, additionalData[:n]...)

		config, format, err := getImageConfig(readData)
		if err == nil {
			return config, format, nil
		}
		decodeErr = err
		if readErr != nil {
			if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
				return image.Config{}, "", decodeErr
			}
			return image.Config{}, "", fmt.Errorf("failed to read image header: %w", readErr)
		}
	}

	return image.Config{}, "", fmt.Errorf("failed to decode image config within 65536-byte header limit: %w", decodeErr)
}

func getImageConfig(data []byte) (image.Config, string, error) {
	// 读取图片的头部信息来获取图片尺寸
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err == nil {
		return config, format, nil
	}
	common.SysLog(fmt.Sprintf("fail to decode image config(gif, jpg, png): %s", err.Error()))

	config, err = webp.DecodeConfig(bytes.NewReader(data))
	if err == nil {
		return config, "webp", nil
	}
	common.SysLog(fmt.Sprintf("fail to decode image config(webp): %s", err.Error()))

	// Try HEIF/HEIC: parse ISOBMFF ispe box for dimensions
	if heifMime := detectHEIF(data); heifMime != "" {
		formatName := "heif"
		if heifMime == "image/heic" {
			formatName = "heic"
		}
		if w, h, ok := parseHEIFDimensions(data); ok {
			return image.Config{Width: w, Height: h}, formatName, nil
		}
		return image.Config{}, "", fmt.Errorf("failed to decode HEIF/HEIC image dimensions")
	}

	return image.Config{}, "", err
}
