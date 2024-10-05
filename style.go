package main

import (
	"gioui.org/layout"
	"gioui.org/unit"
)

var (
	bgSender = Background{
		Color:  th.ContrastBg,
		Inset:  layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(12)},
		Radius: unit.Dp(10),
	}

	bgReceiver = Background{
		Color:  th.ContrastFg,
		Inset:  layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(8)},
		Radius: unit.Dp(10),
	}
	inbetween = layout.Inset{Top: unit.Dp(2)}

	bgl = Background{
		Color: th.Bg,
		Inset: layout.Inset{Top: unit.Dp(0), Bottom: unit.Dp(0), Left: unit.Dp(0), Right: unit.Dp(0)},
	}

	bg = Background{
		Color: th.ContrastBg,
		Inset: layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(0), Left: unit.Dp(12), Right: unit.Dp(12)},
	}

	bgList = Background{
		Color: th.Bg,
		Inset: layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(0), Left: unit.Dp(0), Right: unit.Dp(0)},
	}
)
