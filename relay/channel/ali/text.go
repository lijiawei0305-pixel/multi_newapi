package ali

import "github.com/QuantumNous/new-api/dto"

// https://help.aliyun.com/document_detail/613695.html?spm=a2c4g.2399480.0.0.1adb778fAdzP9w#341800c0f8w0r

const EnableSearchModelSuffix = "-internet"

func requestOpenAI2Ali(request dto.GeneralOpenAIRequest) *dto.GeneralOpenAIRequest {
	if request.TopP != nil && *request.TopP >= 1 {
		topP := 0.999
		request.TopP = &topP
	}
	return &request
}
