package resp

//	       SUCCESS(200,"成功"),
//		   CREATED(201,"数据创建成功"),
//		   FAIL(400, "失败"),
//		   AUTH_FAIL(401,"鉴权失败"),
//		   NOT_AUTH(403,"拒绝接入"),
//		   NOT_FOUND(404,"指定资源不存在"),
//		   OUT_OF_BOUNDS(413,"请求过大"),
//		   SERVER_ERROR(500,"服务器错误"),
//		   NOT_IMPLEMENTED(501,"服务器不支持处理当前请求"),
//		   UNAVAILABLE(503,"服务不可用"),
const (
	SuccessCode     = 200
	CreatedCode     = 201
	FailCode        = 400
	AuthFail        = 401
	NotAuth         = 403
	NotFound        = 404
	OutOfBounds     = 413
	ServerErrorCode = 500
	NotImpl         = 501
	Unavailable     = 503
)

type StatusCode int

func (s StatusCode) Int() int {
	return int(s)
}

func (s StatusCode) String() string {
	switch s {
	case SuccessCode:
		return "成功"
	case CreatedCode:
		return "数据创建成功"
	case FailCode:
		return "失败"
	case AuthFail:
		return "鉴权失败"
	case NotAuth:
		return "拒绝接入"
	case NotFound:
		return "指定资源不存在"
	case OutOfBounds:
		return "请求过大"
	case ServerErrorCode:
		return "服务器错误"
	case NotImpl:
		return "服务器不支持处理当前请求"
	case Unavailable:
		return "服务不可用"
	default:
		return "未知错误"
	}
}
