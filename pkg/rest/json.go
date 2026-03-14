package resp

import (
	"github.com/gin-gonic/gin"
)

// OkJson
// 正确结果封装
func OkJson(c *gin.Context, data any, opts ...ResOpt) {
	r := NewResult().OkWithData(data)
	for _, opt := range opts {
		opt(r)
	}
	c.JSON(200, r)
}

// BadJson
// 失败结果封装
func BadJson(c *gin.Context, data any, err error, opts ...ResOpt) {
	r := NewResult().FailWithData(data)
	for _, opt := range opts {
		opt(r)
	}
	r.Message = err.Error()
	c.JSON(200, r)
}

// CommJson
// 通用结果封装
func CommJson(c *gin.Context, code int, message string, data any, opts ...ResOpt) {
	r := NewResult().buildWithCode(nil, StatusCode(code))
	if message != "" {
		r.Message = message
	}
	if data != nil {
		r.Data = data
	}
	for _, opt := range opts {
		opt(r)
	}
	c.JSON(200, r)
}

type JsonResultWrapper func(c *gin.Context) (any, error)

func JsonWrapper(fun JsonResultWrapper) gin.HandlerFunc {
	return func(context *gin.Context) {
		data, err := fun(context)
		if err != nil {
			BadJson(context, data, err)
		} else {
			OkJson(context, data)
		}
	}
}

// NewJsonHandler
// Json结果封装
func NewJsonHandler(fun func() (method, relativePath string, wrapper JsonResultWrapper, opts []WrapperOption)) *Handler {
	method, path, wrapper, opts := fun()
	return NewHandler(method, path, JsonWrapper(wrapper), opts...)
}

type JsonOptionsResultWrapper func(c *gin.Context) (any, []ResOpt, error)

func JsonOptionsWrapper(fun JsonOptionsResultWrapper) gin.HandlerFunc {
	return func(context *gin.Context) {
		data, opts, err := fun(context)
		if err != nil {
			BadJson(context, data, err, opts...)
		} else {
			OkJson(context, data, opts...)
		}
	}
}

// NewJsonOptionsHandler
// Json结果带选项结果封装
func NewJsonOptionsHandler(fun func() (method, relativePath string, wrapper JsonOptionsResultWrapper, opts []WrapperOption)) *Handler {
	method, path, wrapper, opts := fun()
	return NewHandler(method, path, JsonOptionsWrapper(wrapper), opts...)
}
