package service

import (
	"strings"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func submissionNotice(recipients ...uint) FormSubmissionNotice {
	return FormSubmissionNotice{WorkspaceID: 9, FormID: 5, FormName: "Contact us", SubmissionID: 7, Recipients: recipients}
}

func TestFormSubmissionNotifiesListedMembersInApp(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 10)
	rig.dispatcher.NotifyFormSubmission(submissionNotice(2, 4))
	rig.dispatcher.Wait()

	types := draftTypes(rig.inbox.drafts)
	if len(types) != 2 || types[2] != domain.NotificationFormSubmitted || types[4] != domain.NotificationFormSubmitted {
		t.Fatalf("drafts = %+v", rig.inbox.drafts)
	}
	draft := rig.inbox.drafts[0]
	payload := decodeValue(t, draft.Payload)
	if draft.TaskID != 0 || draft.ActorID != 0 || draft.WorkspaceID != 9 ||
		payload["form_id"] != float64(5) || payload["submission_id"] != float64(7) || payload["form_name"] != "Contact us" {
		t.Fatalf("draft = %+v payload = %v", draft, payload)
	}
	if _, leaked := payload["recipients"]; leaked {
		t.Fatalf("recipients must not be part of the payload: %v", payload)
	}
	if len(rig.mailbox.Messages()) != 0 {
		t.Fatal("e-mail is off by default for form submissions")
	}
}

func TestFormSubmissionSkipsMembersWhoLostWorkspaceAccess(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{deniedWorkspace: map[uint]bool{4: true}}, 10)
	rig.dispatcher.NotifyFormSubmission(submissionNotice(2, 4))
	rig.dispatcher.Wait()
	types := draftTypes(rig.inbox.drafts)
	if len(types) != 1 || types[2] == "" {
		t.Fatalf("drafts = %+v", rig.inbox.drafts)
	}
}

func TestFormSubmissionWithoutRecipientsIsIgnored(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 10)
	rig.dispatcher.NotifyFormSubmission(submissionNotice())
	rig.dispatcher.Wait()
	if len(rig.inbox.drafts) != 0 {
		t.Fatalf("drafts = %+v", rig.inbox.drafts)
	}
}

func TestFormSubmissionMailLinksToTheSubmission(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 10)
	rig.settings.prefs[2] = []domain.NotificationPreference{
		{Type: domain.NotificationFormSubmitted, Channel: domain.ChannelKindEmail, Enabled: true},
	}
	rig.dispatcher.NotifyFormSubmission(submissionNotice(2))
	rig.dispatcher.Wait()

	messages := rig.mailbox.To("two@example.test")
	if len(messages) != 1 {
		t.Fatalf("messages = %+v", rig.mailbox.Messages())
	}
	if messages[0].Subject != `New submission in the form "Contact us"` {
		t.Fatalf("subject = %q", messages[0].Subject)
	}
	if !strings.Contains(messages[0].Text, "https://app.example.test/workspaces/9/forms/5/submissions/7") {
		t.Fatalf("body = %q", messages[0].Text)
	}
}

func TestFormSubmissionWordingIsLocalised(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 10)
	rig.settings.locale[2] = "ru"
	rig.settings.prefs[2] = []domain.NotificationPreference{
		{Type: domain.NotificationFormSubmitted, Channel: domain.ChannelKindEmail, Enabled: true},
	}
	rig.dispatcher.NotifyFormSubmission(submissionNotice(2))
	rig.dispatcher.Wait()
	messages := rig.mailbox.To("two@example.test")
	if len(messages) != 1 || messages[0].Subject != "Новый ответ в форме «Contact us»" {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestNotificationLinkFallsBackToTheTask(t *testing.T) {
	task := notificationLink("https://app.example.test", NotificationContent{TaskID: 12})
	if task != "https://app.example.test/tasks/12" {
		t.Fatalf("link = %q", task)
	}
}
