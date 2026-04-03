package skills

import "errors"

var ErrSkillNotFound = errors.New("skill not found")
var ErrInvalidSkillName = errors.New("invalid skill name")
var ErrInvalidSkillDocument = errors.New("invalid skill document")
