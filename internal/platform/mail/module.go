package mail

import "go.uber.org/fx"

var Module = fx.Module(
	"mail",

	fx.Provide(
		LoadConfig,
		fx.Annotate(NewSMTPSender, fx.As(new(Sender))),
	),
)
