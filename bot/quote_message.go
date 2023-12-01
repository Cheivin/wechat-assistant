package bot

import "github.com/eatmoreapple/openwechat"

const (
	quotePrefix = "「"
	quoteSuffix = "」\n- - - - - - - - - - - - - - -\n"
	QuoteKey    = "quote"
)

type QuoteMessageInfo struct {
	Content string
	Quote   string
	User    *openwechat.User
}
