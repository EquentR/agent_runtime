package resp

import (
	"strconv"
	"time"
)

type ResOpt func(r *Result)

type Result struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
	OK      bool   `json:"ok"`
	Time    string `json:"time"`
}

func NewResult() *Result {
	return &Result{
		Time: time.Now().Format("2006-01-02 15:04:05"),
	}
}

func (r *Result) build(data any) *Result {
	if data != nil {
		r.Data = data
	}
	return r
}

func (r *Result) buildWithCode(data any, code StatusCode) *Result {
	if data != nil {
		r.Data = data
	}
	r.Code = int(code)
	r.Message = code.String()
	r.IsOk()
	return r
}

func (r *Result) buildWithCodeAndMessage(code int, message string) *Result {
	r.Code = code
	r.Message = message
	r.IsOk()
	return r
}

func (r *Result) Ok() *Result {
	return r.OkWithData(nil)
}

func (r *Result) OkWithData(data any) *Result {
	result := r.buildWithCode(data, SuccessCode)
	return result
}

func (r *Result) Fail() *Result {
	return r.FailWithData(nil)
}

func (r *Result) FailWithData(data any) *Result {
	result := r.buildWithCode(data, FailCode)
	return result
}

func (r *Result) SetMessage(msg string) *Result {
	r.Message = msg
	return r
}

func (r *Result) SetCode(code int) *Result {
	r.Code = code
	r.IsOk()
	return r
}

func (r *Result) IsOk() {
	codeStr := strconv.Itoa(r.Code)
	r.OK = codeStr[0] == '2' || codeStr[0] == '3'
}

// WithCode
// 设置code
func WithCode(code int) ResOpt {
	return func(r *Result) {
		r.SetCode(code)
	}
}

// WithMessage
// 设置message
func WithMessage(msg string) ResOpt {
	return func(r *Result) {
		r.SetMessage(msg)
	}
}

// WithCodeToMessage
func WithCodeToMessage(code StatusCode) ResOpt {
	return func(r *Result) {
		r.SetCode(int(code))
		r.SetMessage(code.String())
	}
}
