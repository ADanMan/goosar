package channel

import "context"

type InboundHandler func(ctx context.Context, msg InboundMessage) error
