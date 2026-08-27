package main

import (
	"fmt"
	"log"
	"net/smtp"
	"os"
)

type Mailer interface {
	Envoyer(dest, sujet, corps string) error
}

// MailerJournal : sans configuration SMTP, les courriels sont simplement journalisés.
type MailerJournal struct{}

func (MailerJournal) Envoyer(dest, sujet, corps string) error {
	log.Printf("[courriel simulé] à %s — %s", dest, sujet)
	return nil
}

type MailerSMTP struct {
	hote, port, utilisateur, mdp, de string
}

func (m MailerSMTP) Envoyer(dest, sujet, corps string) error {
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		m.de, dest, sujet, corps)
	var auth smtp.Auth
	if m.utilisateur != "" {
		auth = smtp.PlainAuth("", m.utilisateur, m.mdp, m.hote)
	}
	return smtp.SendMail(m.hote+":"+m.port, auth, m.de, []string{dest}, []byte(msg))
}

func mailerDepuisEnv() Mailer {
	hote := os.Getenv("PERCHES_SMTP_HOTE")
	if hote == "" {
		return MailerJournal{}
	}
	return MailerSMTP{
		hote:        hote,
		port:        env("PERCHES_SMTP_PORT", "587"),
		utilisateur: os.Getenv("PERCHES_SMTP_UTILISATEUR"),
		mdp:         os.Getenv("PERCHES_SMTP_MDP"),
		de:          env("PERCHES_SMTP_DE", "perches@localhost"),
	}
}
