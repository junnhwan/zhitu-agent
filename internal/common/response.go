package common

import "net/http"

// BaseResponse mirrors Java BaseResponse — only used for error responses
// Success responses return plain text per the mixed response contract
type BaseResponse struct {
	Code    int         `json:"code"`
	Data    interface{} `json:"data"`
	Message string      `json:"message"`
}

// Success creates a success response with data
func Success(data interface{}) *BaseResponse {
	return &BaseResponse{Code: 0, Data: data, Message: "ok"}
}

// Error creates an error response from ErrorCode
func Error(errCode ErrorCode, msg string) *BaseResponse {
	if msg == "" {
		msg = errCode.Message
	}
	return &BaseResponse{Code: errCode.Code, Data: nil, Message: msg}
}

// ErrorWithCode creates an error response with explicit code and message
func ErrorWithCode(code int, msg string) *BaseResponse {
	return &BaseResponse{Code: code, Data: nil, Message: msg}
}

// HTTPStatus maps ErrorCode to HTTP status code
func HTTPStatus(errCode ErrorCode) int {
	switch {
	case errCode.Code >= 40000 && errCode.Code < 50000:
		return http.StatusBadRequest
	case errCode.Code >= 50000 && errCode.Code < 60000:
		return http.StatusInternalServerError
	case errCode.Code >= 80000:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
