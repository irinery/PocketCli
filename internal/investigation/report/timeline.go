package report

import "pocketcli/internal/investigation/insightcontract"

func timelineRationaleMetadata(item insightcontract.TimelineItem) []string {
	var out []string
	if item.Rationale != "" || item.RationaleBasis != "" {
		prefix := "Motivo não verificado"
		switch item.RationaleBasis {
		case "source":
			prefix = "Motivo declarado na fonte"
		case "hypothesis":
			prefix = "Hipótese de intenção (confiança baixa)"
		}
		text := item.Rationale
		if text == "" {
			text = "não identificado no registro"
		}
		out = append(out, prefix+": "+text)
	}
	for _, alternative := range item.Alternatives {
		out = append(out, "Outra explicação possível: "+alternative)
	}
	return append(out, item.Limitations...)
}
