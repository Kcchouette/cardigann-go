// Package selector fournit une abstraction commune aux différents modes
// d'extraction Cardigann (CSS pour HTML, JSON path pour JSON, XML via etree).
package selector

// Document est l'abstraction d'un contenu de réponse parsé, quel que soit son format.
type Document interface {
	// Find retourne les noeuds correspondant au sélecteur donné.
	Find(selector string) ([]Node, error)
}

// Node représente un noeud/élément extrait, avec accès au texte et aux attributs.
type Node interface {
	Text() string
	Attr(name string) (string, bool)
}

// SubSelectable est implémenté par les noeuds capables de ré-appliquer un
// sélecteur relativement à eux-mêmes (ex: sélectionner un champ à l'intérieur
// d'une ligne de résultat déjà sélectionnée).
type SubSelectable interface {
	Find(selector string) ([]Node, error)
}
