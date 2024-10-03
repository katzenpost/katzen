package main

import (
	"sort"
)

type sortedContacts []*Contact

func (s sortedContacts) Less(i, j int) bool {
	// sorts contacts by Nickname
	// without messages alphabetically
	return s[i].Nickname < s[j].Nickname
}

func (s sortedContacts) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s sortedContacts) Len() int {
	return len(s)
}

type sortedConvos []*Conversation

func (s sortedConvos) Less(i, j int) bool {
	// sorts conversations by most-recent-first, followed by conversations
	// without messages by title alphabetically

	li := len(s[i].Messages)
	lj := len(s[j].Messages)

	if li == 0 && lj == 0 {
		return s[i].Title < s[j].Title
	} else if li == 0 {
		return false
	} else if lj == 0 {
		return true
	} else {
		return s[i].LastMessage.Before(s[j].LastMessage)
	}
}

func (s sortedConvos) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s sortedConvos) Len() int {
	return len(s)
}

func (a *App) getSortedContacts() (contacts sortedContacts) {
	for _, id := range a.GetContactIDs() {
		contact, err := a.GetContact(id)
		if err != nil {
			contacts = append(contacts, contact)
		}
	}
	sort.Sort(contacts)
	return
}

func (a *App) getSortedConvos() (convos sortedConvos) {
	for _, id := range a.GetConversationIDs() {
		convo, err := a.GetConversation(id)
		if err == nil {
			convos = append(convos, convo)
		}
	}
	sort.Sort(convos)
	return
}
