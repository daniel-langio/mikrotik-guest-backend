package main

import (
	"net/http"
	"sort"
)

type openAPRow struct {
	Name, Err string
	On        bool
}

// renderOpenAP reads each router's live state (no DB copy to drift).
func (a *App) renderOpenAP(w http.ResponseWriter, r *http.Request) {
	var rows []openAPRow
	for name, mt := range a.routers {
		on, err := mt.OpenAP()
		row := openAPRow{Name: name, On: on}
		if err != nil {
			row.Err = err.Error()
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	render(w, r, "admin_openap.html", map[string]any{"Routers": rows})
}

func (a *App) adminOpenAP(w http.ResponseWriter, r *http.Request) { a.renderOpenAP(w, r) }

func (a *App) adminOpenAPSet(w http.ResponseWriter, r *http.Request) {
	mt, ok := a.routers[r.PathValue("router")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := mt.SetOpenAP(r.PathValue("state") == "on"); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	a.renderOpenAP(w, r)
}
