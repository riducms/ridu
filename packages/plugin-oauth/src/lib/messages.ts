import { defineAdminMessages } from "@riducms/plugin";

/** English fallback text for the consent page, translatable under `plugin.oauth:<key>`. */
export const oauthMessages = defineAdminMessages({
	fallback: {
		title: "Connect {client}",
		description:
			"{client} wants to use {application}. It can do anything the person it acts as can, until you disconnect it.",
		actAs: "Acts as",
		you: "{label} (you)",
		returnTo: "After you decide, you’ll return to {host}.",
		returnToLoopback: "After you decide, you’ll return to an app on this computer ({host}).",
		allow: "Allow",
		deny: "Deny",
		loading: "Checking the request…",
		redirecting: "Returning to the app…",
		refusedTitle: "This app can’t connect",
		backToApp: "Back to the app",
		failed: "The request couldn’t be checked: {message}",
		retry: "Try again",
		disconnectHint: "You can disconnect it at any time under Connected apps.",
	},
	translations: {
		fr: {
			title: "Connecter {client}",
			description:
				"{client} souhaite utiliser {application}. Il peut faire tout ce que peut faire la personne au nom de laquelle il agit, jusqu’à ce que vous le déconnectiez.",
			actAs: "Agit en tant que",
			you: "{label} (vous)",
			returnTo: "Après votre choix, vous reviendrez sur {host}.",
			returnToLoopback:
				"Après votre choix, vous reviendrez à une application sur cet ordinateur ({host}).",
			allow: "Autoriser",
			deny: "Refuser",
			loading: "Vérification de la demande…",
			redirecting: "Retour à l’application…",
			refusedTitle: "Cette application ne peut pas se connecter",
			backToApp: "Retour à l’application",
			failed: "Impossible de vérifier la demande : {message}",
			retry: "Réessayer",
			disconnectHint: "Vous pouvez la déconnecter à tout moment dans Applications connectées.",
		},
		ar: {
			title: "ربط {client}",
			description:
				"يريد {client} استخدام {application}. يمكنه فعل كل ما يستطيعه الشخص الذي يعمل باسمه، إلى أن تفصله.",
			actAs: "يعمل باسم",
			you: "{label} (أنت)",
			returnTo: "بعد أن تقرّر، ستعود إلى {host}.",
			returnToLoopback: "بعد أن تقرّر، ستعود إلى تطبيق على هذا الحاسوب ({host}).",
			allow: "سماح",
			deny: "رفض",
			loading: "جارٍ التحقق من الطلب…",
			redirecting: "جارٍ العودة إلى التطبيق…",
			refusedTitle: "لا يمكن لهذا التطبيق الاتصال",
			backToApp: "العودة إلى التطبيق",
			failed: "تعذّر التحقق من الطلب: {message}",
			retry: "حاول مجددًا",
			disconnectHint: "يمكنك فصله في أي وقت من التطبيقات المتصلة.",
		},
	},
});
