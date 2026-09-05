package infra

import "net/http"

func (c *Client) reportProbe(err error, endpoint, stage string, response *http.Response) {
	diagnostic := HTTPDiagnostic(http.MethodGet, endpoint, stage, response)
	diagnostic.Operation, diagnostic.Severity = "probe", DiagnosticWarn
	c.probeDiagnostic.Report(c.log, err, "HTTP", diagnostic)
}

func (c *Client) rewriteTimeout(response *http.Response) *http.Response {
	if response != nil && response.StatusCode == 524 {
		method, endpoint := "", ""
		if response.Request != nil {
			method, endpoint = response.Request.Method, response.Request.URL.String()
		}
		diagnostic := HTTPDiagnostic(method, endpoint, "rewrite-timeout", response)
		diagnostic.Severity = DiagnosticWarn
		_ = ReportError(c.log, &HTTPError{Status: response.StatusCode}, "HTTP", diagnostic)
	}
	return rewriteCloudflareTimeout(response)
}
