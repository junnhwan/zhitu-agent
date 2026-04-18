package channel

import "context"

type domainCtxKey struct{}

// WithDomain 把 intent domain (KNOWLEDGE / TOOL / CHITCHAT / "") 放进 ctx，
// 供 Channel 实现按需读取。零值字符串表示未知域。
func WithDomain(ctx context.Context, domain string) context.Context {
	return context.WithValue(ctx, domainCtxKey{}, domain)
}

func DomainFromContext(ctx context.Context) string {
	v, _ := ctx.Value(domainCtxKey{}).(string)
	return v
}
