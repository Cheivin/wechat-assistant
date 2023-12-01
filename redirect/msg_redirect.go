package redirect

type (
	MsgRedirect interface {
		RedirectCommand(CommandMessage) bool
		RedirectMessage(*Message) bool
		SetCommandHandler(func(BotCommand))
	}
	Message struct {
		MsgID      string  `json:"msgID"`
		UID        string  `json:"uid"`
		Username   string  `json:"username"`
		GID        string  `json:"gid"`
		GroupName  string  `json:"groupName"`
		RawMessage string  `json:"rawMessage,omitempty"`
		MsgType    int     `json:"msgType"`
		Time       int64   `json:"time"`
		Quote      *Quote  `json:"quote,omitempty"`
		Revoke     *Revoke `json:"revoke,omitempty"`
	}
	Quote struct {
		UID   string `json:"uid"`
		Quote string `json:"quote"`
	}
	Revoke struct {
		OldMsgID   string `json:"oldMsgID"`
		ReplaceMsg string `json:"replaceMsg"`
	}

	CommandMessage struct {
		Message
		Command string `json:"message"`
	}

	BotCommand struct {
		Command string         `json:"command"`
		Param   SendMsgCommand `json:"param"`
	}

	SendMsgCommand struct {
		Gid       string `json:"gid" form:"gid"`           // 群id
		GroupName string `json:"groupName" form:"gid"`     // 群名称
		Type      int    `json:"type" form:"type"`         // 回复类型 1:文本,2:图片,3:视频,4:文件
		Body      string `json:"body" form:"body"`         // 回复内容,type=1时为文本内容,type=2/3/4时为资源地址
		Filename  string `json:"filename" form:"filename"` // 文件名称
		Prompt    string `json:"prompt" form:"prompt"`     // 回复提示
	}
)
