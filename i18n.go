package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

// French (default) and Malagasy. The language lives in a "lang" cookie set by
// POST /lang; templates call {{t "key" args...}}, Go code calls tr(lang, key).
var dict = map[string]map[string]string{
	"fr": {
		"welcome.title":       "Bienvenue",
		"welcome.subtitle":    "Portail invité Aphrodite",
		"admin.wait":          "En attente de l'approbation d'un administrateur…",
		"admin.auto":          "Vous serez connecté automatiquement dès l'approbation - inutile d'actualiser.",
		"btn.approve_now":     "Approuver maintenant",
		"pay.hint":            "Payez par Mobile Money pour être connecté sans attendre.",
		"pay.rate":            "%d Ar = %d min.",
		"pay.send_to":         "Envoyez l'argent au",
		"pay.then":            ", puis saisissez la référence du SMS de confirmation.",
		"pay.mode_time":       "Choisir par durée",
		"pay.mode_price":      "Choisir par prix",
		"pay.price":           "Prix (Ar)",
		"pay.minutes":         "Minutes",
		"pay.phone":           "Votre numéro (034...)",
		"pay.ref":             "Référence du paiement",
		"pay.submit":          "Confirmer le paiement",
		"pay.checking":        "Vérification du paiement",
		"pay.auto_confirm":    "Vous serez connecté automatiquement dès la confirmation - inutile d'actualiser.",
		"pay.wrong_ref":       "Mauvaise référence ?",
		"pay.accepted":        "Paiement accepté",
		"pay.approved":        "Approuvé",
		"pay.redirecting":     "Vous êtes connecté - redirection…",
		"err.amount":          "Le montant doit être un multiple de %d Ar, jusqu'à %d Ar",
		"err.provider":        "Opérateur inconnu",
		"err.phone":           "Numéro de téléphone invalide",
		"err.ref_format":      "Cela ne ressemble pas à une référence %s - copiez-la depuis le SMS de confirmation",
		"err.ref_used":        "Cette référence a déjà été utilisée",
		"err.not_pending":     "Cette session n'est plus en attente",
		"err.ifay":            "Impossible de vérifier le paiement, réessayez : %s",
		"err.username":        "Nom d'utilisateur : 3 à 32 caractères parmi a-z 0-9 _ . -",
		"err.reserved":        "Ce nom d'utilisateur est réservé",
		"err.pwlen":           "Le mot de passe doit contenir au moins 8 caractères",
		"err.userexists":      "Ce nom d'utilisateur existe déjà",
		"err.login":           "Nom d'utilisateur ou mot de passe invalide",
		"login.subtitle":      "Administration du portail invité",
		"login.username":      "Nom d'utilisateur",
		"login.password":      "Mot de passe",
		"login.submit":        "Se connecter",
		"admin.subtitle":      "Portail invité",
		"admin.logout":        "Déconnexion",
		"admin.pending":       "Appareils en attente",
		"admin.active":        "Sessions actives",
		"admin.operators":     "Opérateurs",
		"admin.history":       "Historique des connexions",
		"pending.clear":       "Tout effacer",
		"pending.clear_q":     "Supprimer TOUTES les sessions en attente ?",
		"pending.delete":      "Supprimer",
		"pending.delete_q":    "Supprimer cette session en attente ?",
		"pending.approve":     "Approuver",
		"pending.custom":      "Personnalisé…",
		"pending.2h":          "2 heures",
		"pending.24h":         "24 heures",
		"pending.empty":       "Aucun appareil en attente",
		"active.disc":         "Déconnecter",
		"active.disc_q":       "Déconnecter et supprimer cette session ?",
		"active.empty":        "Aucune session active",
		"users.operator":      "opérateur",
		"users.delete_q":      "Supprimer l'opérateur %s ?",
		"users.empty":         "Aucun opérateur",
		"users.username":      "nom d'utilisateur",
		"users.password":      "mot de passe (8+)",
		"users.add":           "Ajouter un opérateur",
		"history.ok":          "réussie",
		"history.failed":      "échouée",
		"history.empty":       "Aucune connexion",
		"js.unit_h":           "h",
		"js.unit_m":           "min",
		"js.done":             "Fait",
		"js.error":            "Une erreur est survenue",
		"js.neterror":         "Erreur réseau - serveur injoignable",
		"js.expired":          "expiré",
		"js.left":             "restant",
		"help.link":           "Comment ça marche ?",
		"help.title":          "Comment ça marche",
		"help.back":           "Retour",
		"help.s1.title":       "Choisissez votre durée",
		"help.s1.text":        "100 Ar = 3 minutes. Choisissez le prix et la durée s'affiche (ou l'inverse, avec le bouton).",
		"help.s2.title":       "Envoyez l'argent",
		"help.s2.text":        "Faites un transfert Mobile Money (MVola ou Orange Money) vers le numéro affiché.",
		"help.s3.title":       "Repérez la référence",
		"help.s3.text":        "Le SMS de confirmation contient une référence (Ref ou Trans Id).",
		"help.s4.title":       "Saisissez-la",
		"help.s4.text":        "Touchez « Approuver maintenant », entrez la référence et votre numéro, puis confirmez.",
		"help.s5.title":       "Vous êtes connecté !",
		"help.s5.text":        "Dès que le paiement est reconnu, vous êtes connecté automatiquement, sans attendre un administrateur.",
		"help.faq.title":      "Bon à savoir",
		"help.faq1":           "Une référence ne sert qu'une seule fois.",
		"help.faq2":           "Mauvaise référence ? Touchez « Mauvaise référence ? » pour la ressaisir.",
		"help.faq3":           "Payez au moins le montant choisi.",
		"help.faq4":           "Le paiement est vérifié automatiquement, en quelques secondes.",
		"help.sms_note":       "Exemple fictif",
		"open.title":          "Portail ouvert (approbation automatique)",
		"open.state_on":       "OUVERT - chaque nouvel invité reçoit %s automatiquement",
		"open.state_off":      "Fermé - approbation manuelle ou paiement",
		"open.btn_open":       "Ouvrir le portail",
		"open.btn_close":      "Fermer le portail",
		"open.confirm":        "Ouvrir le portail ? Tout invité sera connecté gratuitement, et les invités en attente seront approuvés.",
		"open.1h":             "1 heure",
		"admin.devices":       "Historique des appareils",
		"devices.csv":         "Exporter CSV",
		"devices.empty":       "Aucun appareil enregistré",
		"ev.pending":          "arrivé (en attente)",
		"ev.approved":         "approuvé",
		"ev.expired":          "expiré",
		"ev.disconnected":     "déconnecté",
		"ev.deleted":          "supprimé",
		"by.payment":          "paiement",
		"by.open":             "portail ouvert",
		"by.expiry":           "expiration",
		"open.closes_at":      "Ferme à",
		"open.no_close":       "jusqu'à fermeture manuelle",
		"open.close_in":       "Fermer dans",
		"open.never":          "Jamais (manuel)",
		"open.4h":             "4 heures",
		"open.8h":             "8 heures",
		"open.custom_min":     "min perso",
		"consent.title":       "Wifi gratuit : vous payez avec vos données",
		"consent.lead":        "En échange de l'accès gratuit, nous ENREGISTRONS VOTRE ACTIVITÉ sur ce wifi. Nous collectons tout ce qu'un opérateur réseau peut voir :",
		"consent.item1":       "Votre appareil : adresse MAC, adresse IP, nom de l'appareil, système, navigateur.",
		"consent.item2":       "Vos connexions : heures de connexion et de déconnexion, volume de données.",
		"consent.item3":       "Chaque site ou serveur contacté : nom de domaine, adresse IP, port, heure, volume envoyé et reçu.",
		"consent.item4":       "Votre numéro de téléphone et vos références de paiement, si vous payez.",
		"consent.not":         "Nous ne lisons PAS le contenu de vos pages, vos messages ni vos mots de passe (chiffrés par HTTPS).",
		"consent.why":         "Pourquoi : statistiques, analyse commerciale et publicité. Des statistiques agrégées peuvent être partagées avec des partenaires.",
		"consent.keep":        "Conservation : %d jours, puis suppression automatique.",
		"consent.rights":      "Vous pouvez retirer votre consentement et faire supprimer vos données à tout moment ; la connexion gratuite s'arrête alors.",
		"consent.contact":     "Contact : %s",
		"consent.policy_link": "Lire la politique complète",
		"consent.checkbox":    "J'ai lu et j'accepte que mon activité soit enregistrée.",
		"consent.accept":      "Accepter et me connecter gratuitement",
		"consent.pay_instead": "Je préfère payer (sans enregistrement)",
		"privacy.title":       "Politique de confidentialité",
		"privacy.link":        "Confidentialité",
		"privacy.legal":       "Base légale : votre consentement. Vos droits : accès, rectification, suppression et retrait du consentement.",
		"privacy.version":     "Version de la politique :",
		"privacy.withdraw":    "Retirer mon consentement et supprimer mes données",
		"privacy.withdrawn":   "C'est fait : votre consentement est retiré, vos données d'activité sont supprimées et la connexion gratuite est terminée.",
		"admin.activity":      "Activité des invités",
		"activity.empty":      "Aucune activité enregistrée",
		"ev.consent":          "consentement donné",
		"ev.withdrawn":        "consentement retiré",
		"pager.prev":          "Précédent",
		"pager.next":          "Suivant",
		"pager.page":          "Page %d",
	},
	"mg": {
		"welcome.title":       "Tongasoa",
		"welcome.subtitle":    "Vavahady vahiny Aphrodite",
		"admin.wait":          "Miandry ny fankatoavan'ny mpitantana ny fidirinao…",
		"admin.auto":          "Hifandray ho azy ianao rehefa neken'ny mpitantana - tsy mila manavao ny pejy.",
		"btn.approve_now":     "Ankatoavo izao",
		"pay.hint":            "Mandoa amin'ny Mobile Money mba hifandray avy hatrany.",
		"pay.rate":            "%d Ar = %d minitra.",
		"pay.send_to":         "Alefaso any amin'ny",
		"pay.then":            ", dia ampidiro ny Ref ao amin'ny SMS fanamarinana.",
		"pay.mode_time":       "Fidio araka ny fotoana",
		"pay.mode_price":      "Fidio araka ny vidiny",
		"pay.price":           "Vidiny (Ar)",
		"pay.minutes":         "Minitra",
		"pay.phone":           "Ny laharanao (034...)",
		"pay.ref":             "Ref ny fandoavam-bola",
		"pay.submit":          "Hamafiso ny fandoavam-bola",
		"pay.checking":        "Fanamarinana ny fandoavam-bola",
		"pay.auto_confirm":    "Hifandray ho azy ianao rehefa voamarina - tsy mila manavao ny pejy.",
		"pay.wrong_ref":       "Diso ve ny Ref?",
		"pay.accepted":        "Voaray ny fandoavam-bola",
		"pay.approved":        "Nekena",
		"pay.redirecting":     "Mifandray ianao ankehitriny - mitodika…",
		"err.amount":          "Ny vola dia tsy maintsy mizara tsara amin'ny %d Ar, hatramin'ny %d Ar",
		"err.provider":        "Mpandraharaha tsy fantatra",
		"err.phone":           "Laharana finday diso",
		"err.ref_format":      "Tsy mitovy amin'ny Ref %s izany - adikao avy amin'ny SMS fanamarinana",
		"err.ref_used":        "Efa nampiasaina io Ref io",
		"err.not_pending":     "Tsy miandry intsony ity session ity",
		"err.ifay":            "Tsy afaka nanamarina ny fandoavam-bola, andramo indray: %s",
		"err.username":        "Anaran'ny mpampiasa: tarehintsoratra 3 ka hatramin'ny 32 amin'ny a-z 0-9 _ . -",
		"err.reserved":        "Voatokana io anarana io",
		"err.pwlen":           "Tsy maintsy misy tarehintsoratra 8 farafahakeliny ny tenimiafina",
		"err.userexists":      "Efa misy io anarana io",
		"err.login":           "Diso ny anarana na ny tenimiafina",
		"login.subtitle":      "Fitantanana ny vavahady vahiny",
		"login.username":      "Anaran'ny mpampiasa",
		"login.password":      "Tenimiafina",
		"login.submit":        "Hiditra",
		"admin.subtitle":      "Vavahady vahiny",
		"admin.logout":        "Hivoaka",
		"admin.pending":       "Fitaovana miandry",
		"admin.active":        "Session mavitrika",
		"admin.operators":     "Mpiasa",
		"admin.history":       "Tantaran'ny fidirana",
		"pending.clear":       "Fafana daholo",
		"pending.clear_q":     "Fafana ny session rehetra miandry?",
		"pending.delete":      "Fafana",
		"pending.delete_q":    "Fafana ity session miandry ity?",
		"pending.approve":     "Ankatoavo",
		"pending.custom":      "Manokana…",
		"pending.2h":          "2 ora",
		"pending.24h":         "24 ora",
		"pending.empty":       "Tsy misy fitaovana miandry",
		"active.disc":         "Hamoaka",
		"active.disc_q":       "Hamoaka sy hamafa ity session ity?",
		"active.empty":        "Tsy misy session mavitrika",
		"users.operator":      "mpiasa",
		"users.delete_q":      "Fafana ny mpiasa %s?",
		"users.empty":         "Tsy misy mpiasa",
		"users.username":      "anaran'ny mpampiasa",
		"users.password":      "tenimiafina (8+)",
		"users.add":           "Hanampy mpiasa",
		"history.ok":          "tafita",
		"history.failed":      "tsy tafita",
		"history.empty":       "Tsy mbola misy fidirana",
		"js.unit_h":           "ora",
		"js.unit_m":           "min",
		"js.done":             "Vita",
		"js.error":            "Nisy olana",
		"js.neterror":         "Olana tamin'ny tambajotra - tsy tonga any amin'ny server",
		"js.expired":          "lany daty",
		"js.left":             "sisa",
		"help.link":           "Ahoana ny fampiasana?",
		"help.title":          "Ahoana ny fampiasana",
		"help.back":           "Hiverina",
		"help.s1.title":       "Fidio ny fotoana",
		"help.s1.text":        "100 Ar = 3 minitra. Fidio ny vidiny dia hiseho ny fotoana (na ny mifamadika, amin'ny bokotra).",
		"help.s2.title":       "Alefaso ny vola",
		"help.s2.text":        "Manaova famindrana vola Mobile Money (MVola na Orange Money) mankany amin'ny laharana aseho.",
		"help.s3.title":       "Jereo ny Ref",
		"help.s3.text":        "Ao amin'ny SMS fanamarinana misy Ref (na Trans Id).",
		"help.s4.title":       "Ampidiro izy",
		"help.s4.text":        "Tsindrio ny « Ankatoavo izao », ampidiro ny Ref sy ny laharanao, dia hamafiso.",
		"help.s5.title":       "Mifandray ianao!",
		"help.s5.text":        "Raha voamarina ny fandoavam-bola dia mifandray ho azy ianao, tsy miandry mpitantana.",
		"help.faq.title":      "Tsara ho fantatra",
		"help.faq1":           "Ref iray dia fampiasa indray mandeha monja.",
		"help.faq2":           "Diso ny Ref? Tsindrio ny « Diso ve ny Ref? » mba hampidirana indray.",
		"help.faq3":           "Mandoa farafahakeliny ny vola nofidinao.",
		"help.faq4":           "Voamarina ho azy ao anatin'ny segondra vitsy ny fandoavam-bola.",
		"help.sms_note":       "Ohatra foronina",
		"open.title":          "Vavahady misokatra (fankatoavana ho azy)",
		"open.state_on":       "MISOKATRA - mahazo %s ho azy ny vahiny vaovao rehetra",
		"open.state_off":      "Mihidy - fankatoavana tanana na fandoavam-bola",
		"open.btn_open":       "Sokafy ny vavahady",
		"open.btn_close":      "Akatony ny vavahady",
		"open.confirm":        "Sokafana ve ny vavahady? Hifandray maimaim-poana ny vahiny rehetra, ary hekena ny vahiny miandry.",
		"open.1h":             "1 ora",
		"admin.devices":       "Tantaran'ny fitaovana",
		"devices.csv":         "Alaivo CSV",
		"devices.empty":       "Tsy mbola misy fitaovana voasoratra",
		"ev.pending":          "tonga (miandry)",
		"ev.approved":         "nekena",
		"ev.expired":          "lany daty",
		"ev.disconnected":     "nesorina",
		"ev.deleted":          "voafafa",
		"by.payment":          "fandoavam-bola",
		"by.open":             "vavahady misokatra",
		"by.expiry":           "fahataperan'ny fotoana",
		"open.closes_at":      "Hihidy amin'ny",
		"open.no_close":       "mandra-pahatsindrian'ny bokotra hidy",
		"open.close_in":       "Hihidy afaka",
		"open.never":          "Tsy mihidy (tanana)",
		"open.4h":             "4 ora",
		"open.8h":             "8 ora",
		"open.custom_min":     "min manokana",
		"consent.title":       "Wifi maimaim-poana: ny angonao no sarany",
		"consent.lead":        "Ho takalon'ny fidirana maimaim-poana, dia MIRAKITRA NY ASA ATAONAO amin'ity wifi ity izahay. Angonintsika izay rehetra hitan'ny mpandraharaha tambajotra:",
		"consent.item1":       "Ny fitaovanao: adiresy MAC, adiresy IP, anaran'ny fitaovana, rafitra, navigateur.",
		"consent.item2":       "Ny fifandraisanao: ora nidirana sy nivoahana, habetsaky ny data.",
		"consent.item3":       "Ny tranonkala na server rehetra nifandraisanao: anaran'ny domain, adiresy IP, port, ora, habetsaky ny data nalefa sy voaray.",
		"consent.item4":       "Ny laharana findainao sy ny Ref fandoavam-bola, raha mandoa ianao.",
		"consent.not":         "TSY vakintsika ny votoatin'ny pejinao, ny hafatrao na ny tenimiafinao (voaaro amin'ny HTTPS).",
		"consent.why":         "Antony: antontan'isa, famakafakana ara-barotra ary dokambarotra. Mety hozarainay amin'ny mpiara-miasa ny antontan'isa fehiny.",
		"consent.keep":        "Fotoana fitehirizana: %d andro, avy eo fafana ho azy.",
		"consent.rights":      "Azonao esorina ny fanekenao ary alefa fafana ny angonao amin'ny fotoana rehetra; hijanona avy hatrany ny fidirana maimaim-poana.",
		"consent.contact":     "Fifandraisana: %s",
		"consent.policy_link": "Vakio ny politika feno",
		"consent.checkbox":    "Namaky aho ary manaiky fa horaketina ny asa ataoko.",
		"consent.accept":      "Manaiky ary hifandray maimaim-poana",
		"consent.pay_instead": "Aleoko mandoa (tsy misy fandraketana)",
		"privacy.title":       "Politika momba ny tsiambaratelo",
		"privacy.link":        "Tsiambaratelo",
		"privacy.legal":       "Fototra ara-dalàna: ny fanekenao. Ny zonao: fidirana, fanitsiana, famafana ary fanesorana ny faneken'ny.",
		"privacy.version":     "Dikan'ny politika:",
		"privacy.withdraw":    "Esory ny fanekeko ary fafana ny angoko",
		"privacy.withdrawn":   "Vita: voaesotra ny fanekenao, voafafa ny angon'asanao ary tapitra ny fidirana maimaim-poana.",
		"admin.activity":      "Asan'ny vahiny",
		"activity.empty":      "Tsy mbola misy asa voarakitra",
		"ev.consent":          "nanaiky",
		"ev.withdrawn":        "nesorina ny faneken'ny",
		"pager.prev":          "Teo aloha",
		"pager.next":          "Manaraka",
		"pager.page":          "Pejy %d",
	},
}

const defaultLang = "fr"

func langOf(r *http.Request) string {
	if c, err := r.Cookie("lang"); err == nil {
		if _, ok := dict[c.Value]; ok {
			return c.Value
		}
	}
	return defaultLang
}

// tr falls back to French, then to the key itself, so a missing translation
// is visible but never blank.
func tr(lang, key string, args ...any) string {
	s, ok := dict[lang][key]
	if !ok {
		s, ok = dict[defaultLang][key]
	}
	if !ok {
		return key
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// render executes a template with {{t}} bound to the request's language. The
// base set is cloned per request (it's tiny) because FuncMap is per template set.
func render(w http.ResponseWriter, r *http.Request, name string, data any) {
	lang := langOf(r)
	t, err := tmpl.Clone()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t.Funcs(template.FuncMap{
		"t":    func(key string, args ...any) string { return tr(lang, key, args...) },
		"lang": func() string { return lang },
	})
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// setLang stores the choice and returns to the page the click came from
// (same-origin Referer, path + query only, so no open redirect).
func (a *App) setLang(w http.ResponseWriter, r *http.Request) {
	if l := r.FormValue("lang"); dict[l] != nil {
		http.SetCookie(w, &http.Cookie{Name: "lang", Value: l, Path: "/", MaxAge: 365 * 24 * 3600, SameSite: http.SameSiteLaxMode})
	}
	target := "/"
	if u, err := url.Parse(r.Referer()); err == nil {
		if t := u.RequestURI(); strings.HasPrefix(t, "/") && !strings.HasPrefix(t, "//") {
			target = t
		}
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
