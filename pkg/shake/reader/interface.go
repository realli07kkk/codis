package reader

import (
	"github.com/CodisLabs/codis/pkg/shake/entry"
	"github.com/CodisLabs/codis/pkg/shake/status"
	"context"
)

type Reader interface {
	status.Statusable
	StartRead(ctx context.Context) []chan *entry.Entry
}
