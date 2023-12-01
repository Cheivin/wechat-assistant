package bot

type SysMsg struct {
	RevokeMsg struct {
		Session    string `xml:"session"`
		OldMsgID   int64  `xml:"oldmsgid"`
		MsgID      string `xml:"msgid"`
		ReplaceMsg string `xml:"replacemsg"`
	} `xml:"revokemsg"`
}
