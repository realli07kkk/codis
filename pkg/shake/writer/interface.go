package writer

import (
	"github.com/CodisLabs/codis/pkg/shake/entry"
	"github.com/CodisLabs/codis/pkg/shake/status"
	"context"
)

type Writer interface {
	status.Statusable
	Write(entry *entry.Entry)
	StartWrite(ctx context.Context) (ch chan *entry.Entry)
	Close()
}
