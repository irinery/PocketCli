# Artefatos demonstrativos

Os PDFs desta pasta foram gerados pelo teste `TestOfflineRuntimeToExecutiveAndDetailPDF` a partir da fixture sintética `internal/investigation/insightcontract/testdata/fixture_go.json`. Não contêm dados da empresa nem demonstram qualidade de um modelo real. Os IDs e timestamps de execução podem variar entre gerações.

Para recriar:

```sh
POCKETCLI_INSIGHTS_DEMO_DIR="$PWD/output/pdf" go test -buildvcs=false -count=1 -run TestOfflineRuntimeToExecutiveAndDetailPDF ./tests/investigation/insights
```
