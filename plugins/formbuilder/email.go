package formbuilder

import (
	stdcontext "context"
	"fmt"
	"html"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"

	ridu "github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

type emailConfigReadKey struct{}

var emailPlaceholderPattern = regexp.MustCompile(`\{\{(.+?)\}\}`)

type templateValue struct {
	Field string
	Value string
}

// Email is one fully formatted outbound message. Delivery remains application
// owned so provider credentials never enter the plugin manifest or documents.
type Email struct {
	To      string
	CC      string
	BCC     string
	From    string
	ReplyTo string
	Subject string
	HTML    string
}

// EmailContext identifies the committed submission that produced messages.
type EmailContext struct {
	Context    ridu.HookContext
	Form       store.Document
	Submission store.Document
}

// BeforeEmail can wrap templates, filter recipients, or otherwise transform
// the complete detached email batch before delivery.
type BeforeEmail func(EmailContext, []Email) ([]Email, error)

// SendEmail delivers one formatted email using application-owned credentials.
type SendEmail func(stdcontext.Context, Email) error

func (plugin *Plugin) sendSubmissionEmails(context ridu.HookContext) error {
	if context.Operation != operation.Create || plugin.config.SendEmail == nil || context.Document == nil {
		return nil
	}
	formID, valid := relationshipID(context.Document.Values["form"])
	if !valid {
		return nil
	}
	readContext := stdcontext.WithValue(context.Context, emailConfigReadKey{}, true)
	form, err := context.Local.Find(readContext, string(plugin.config.FormsSlug), formID, ridu.FindOptions{Actor: context.Actor, ActorCollection: context.ActorCollection, Locale: context.Locale})
	if err != nil {
		plugin.report(err, "form_builder_email_form_read_failed")
		return nil
	}
	rows, _ := listValue(context.Document.Values, "submissionData")
	templateValues := make([]templateValue, 0, len(rows)+1)
	for _, value := range rows {
		row := value
		if row.Kind() != store.ValueObject {
			continue
		}
		fieldName, _ := row.Get("field").StringValue()
		templateValues = append(templateValues, templateValue{Field: fieldName, Value: scalarString(row.Get("value"))})
	}
	templateValues = append(templateValues, templateValue{Field: "formSubmissionID", Value: context.Document.ID})
	emailRows, _ := listValue(form.Values, "emails")
	emails := make([]Email, 0, len(emailRows))
	for index, value := range emailRows {
		row := value
		if row.Kind() != store.ValueObject {
			continue
		}
		to, _ := row.Get("emailTo").StringValue()
		if strings.TrimSpace(to) == "" {
			to = plugin.config.DefaultToEmail
		}
		cc, _ := row.Get("cc").StringValue()
		bcc, _ := row.Get("bcc").StringValue()
		from, _ := row.Get("emailFrom").StringValue()
		replyTo, _ := row.Get("replyTo").StringValue()
		if strings.TrimSpace(replyTo) == "" {
			replyTo = from
		}
		subject, _ := row.Get("subject").StringValue()
		message, _ := row.Get("message").StringValue()
		email := Email{
			To: replacePlaceholders(to, templateValues, false), CC: replacePlaceholders(cc, templateValues, false), BCC: replacePlaceholders(bcc, templateValues, false),
			From: replacePlaceholders(from, templateValues, false), ReplyTo: replacePlaceholders(replyTo, templateValues, false), Subject: replacePlaceholders(subject, templateValues, false),
			HTML: "<div>" + replacePlaceholders(message, templateValues, true) + "</div>",
		}
		if validationErr := validateEmailHeaders(email); validationErr != nil {
			plugin.report(fmt.Errorf("email %d: %w", index, validationErr), "form_builder_email_invalid")
			continue
		}
		emails = append(emails, email)
	}
	emailContext := EmailContext{Context: context, Form: form, Submission: store.CloneDocument(*context.Document)}
	if plugin.config.BeforeEmail != nil {
		emails, err = plugin.config.BeforeEmail(emailContext, append([]Email(nil), emails...))
		if err != nil {
			plugin.report(err, "form_builder_before_email_failed")
			return nil
		}
	}
	for index, email := range emails {
		if validationErr := validateEmailHeaders(email); validationErr != nil {
			plugin.report(fmt.Errorf("transformed email %d: %w", index, validationErr), "form_builder_email_invalid")
			continue
		}
		if err := plugin.config.SendEmail(context.Context, email); err != nil {
			plugin.report(err, "form_builder_email_delivery_failed")
		}
	}
	return nil
}

func (plugin *Plugin) report(err error, code string) {
	if plugin.config.ReportError != nil {
		plugin.config.ReportError(err, code)
		return
	}
	slog.Error("Ridu Form Builder lifecycle error", "code", code, "error", err)
}

func validateEmailHeaders(email Email) error {
	for label, value := range map[string]string{"to": email.To, "from": email.From} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s address is required", label)
		}
		if _, err := mail.ParseAddressList(value); err != nil {
			return fmt.Errorf("%s address is invalid: %w", label, err)
		}
	}
	for label, value := range map[string]string{"cc": email.CC, "bcc": email.BCC, "reply-to": email.ReplyTo} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, err := mail.ParseAddressList(value); err != nil {
			return fmt.Errorf("%s address is invalid: %w", label, err)
		}
	}
	if strings.ContainsAny(email.Subject, "\r\n") {
		return fmt.Errorf("subject must not contain line breaks")
	}
	return nil
}

func replacePlaceholders(template string, values []templateValue, escapeHTML bool) string {
	if template == "" {
		return ""
	}
	encode := func(value string) string {
		if escapeHTML {
			return html.EscapeString(value)
		}
		return value
	}
	allText := make([]string, 0, len(values))
	allTable := strings.Builder{}
	allTable.WriteString("<table><tbody>")
	for _, variable := range values {
		allText = append(allText, encode(variable.Field)+" : "+encode(variable.Value))
		allTable.WriteString("<tr><td>")
		allTable.WriteString(html.EscapeString(variable.Field))
		allTable.WriteString("</td><td>")
		allTable.WriteString(html.EscapeString(variable.Value))
		allTable.WriteString("</td></tr>")
	}
	allTable.WriteString("</tbody></table>")
	return emailPlaceholderPattern.ReplaceAllStringFunc(template, func(match string) string {
		variable := strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}")
		switch variable {
		case "*":
			return strings.Join(allText, " <br /> ")
		case "*:table":
			return allTable.String()
		}
		for _, value := range values {
			if value.Field == variable {
				return encode(value.Value)
			}
		}
		return variable
	})
}
