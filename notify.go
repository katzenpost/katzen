package main

import (
	"gioui.org/x/notify"
	"time"
)

func shortNotify(title, msg string) {
	go func() {
		if n, err := notify.Push(title, msg); err == nil {
			<-time.After(notificationTimeout)
			n.Cancel()
		}
	}()
}
