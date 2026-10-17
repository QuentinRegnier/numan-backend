package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"os"
	"strconv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/email_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/templates"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/wneessen/go-mail"
)

// ############################################################################
// # WORKER : EMAIL DISPATCHER (MTA CLIENT)
// ############################################################################

// startEmailWorker écoute la file Redis et expédie les emails de manière asynchrone.
func startEmailWorker(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Worker Email (SMTP Dispatcher)...")

	// Timeout de sécurité pour le BLPop (similaire au Push Worker)
	blpopTimeout := 5 * time.Second

	for {
		select {
		case <-ctx.Done():
			numan_log.Info(ctx).Msg("Arrêt gracieux du Worker Email.")
			return // Arrêt gracieux commandé par le manager
		default:
			// Dépilement bloquant sur la queue email
			res, errPop := redis.Rdb.BLPop(ctx, blpopTimeout, variables.EmailWorkerQueueName).Result()
			if errPop != nil {
				continue // File vide ou timeout atteint, on reboucle
			}

			if len(res) == 2 {
				var payload email_models.EmailPayload
				if errUnmarshal := json.Unmarshal([]byte(res[1]), &payload); errUnmarshal == nil {
					processEmailJob(ctx, payload)
				} else {
					numan_log.Warn(ctx).Err(errUnmarshal).Msg("Email Worker : Impossible de désérialiser le job")
				}
			}
		}
	}
}

// processEmailJob orchestre la fusion du template et l'envoi SMTP.
func processEmailJob(ctx context.Context, payload email_models.EmailPayload) {
	// 1. Compilation du Template HTML
	htmlBody, errTpl := parseTemplate(payload.TemplateName, payload.Data)
	if errTpl != nil {
		numan_log.Error(ctx).Err(errTpl).Str("template", payload.TemplateName).Msg("Échec de la compilation du template email")
		return
	}

	// 2. Expédition SMTP
	errSmtp := sendEmail(ctx, htmlBody, payload)
	if errSmtp != nil {
		numan_log.Error(ctx).Err(errSmtp).Str("recipient", payload.RecipientEmail).Msg("Échec critique lors de l'envoi SMTP")
	} else {
		numan_log.Info(ctx).Str("recipient", payload.RecipientEmail).Str("template", payload.TemplateName).Msg("Email expédié avec succès")
	}
}

// parseTemplate fusionne le layout maître, le template spécifique et les données dynamiques.
func parseTemplate(templateName string, data map[string]any) (string, error) {
	// Ajout automatique de l'année pour le footer
	if data == nil {
		data = make(map[string]any)
	}
	data["Year"] = time.Now().Year()

	// ParseFS charge les fichiers directement depuis la RAM (Embedded FS)
	tpl, err := template.ParseFS(templates.EmailFS, "emails/layout.html", "emails/"+templateName+".html")
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	// Remplacer "layout" par "layout.html" :
	if err := tpl.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// sendEmail construit le message MIME et initie la connexion TCP vers le serveur SMTP.
func sendEmail(ctx context.Context, htmlBody string, payload email_models.EmailPayload) error {
	host := os.Getenv("SMTP_HOST")
	portStr := os.Getenv("SMTP_PORT")
	fromAddr := os.Getenv("SMTP_FROM_ADDRESS")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASSWORD")

	port, err := strconv.Atoi(portStr)
	if err != nil || port == 0 {
		port = 2525 // Fallback par défaut (K8s)
	}

	// 1. Construction du Message
	msg := mail.NewMsg()
	if err := msg.From(fromAddr); err != nil {
		return err
	}
	if err := msg.To(payload.RecipientEmail); err != nil {
		return err
	}
	msg.Subject(payload.Subject)
	msg.SetBodyString(mail.TypeTextHTML, htmlBody)

	// 2. Configuration du Client SMTP
	options := []mail.Option{
		mail.WithPort(port),
		mail.WithTLSPolicy(mail.TLSOpportunistic), // STARTTLS si disponible (Maddy), sinon clair (Mailpit local)
	}

	// Si l'authentification est requise (SMTP_USER défini)
	if user != "" && pass != "" {
		options = append(options, mail.WithSMTPAuth(mail.SMTPAuthPlain), mail.WithUsername(user), mail.WithPassword(pass))
	}

	client, err := mail.NewClient(host, options...)
	if err != nil {
		return err
	}

	// 3. Connexion et Expédition
	return client.DialAndSendWithContext(ctx, msg)
}
