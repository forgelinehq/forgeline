package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Amazon Inspector findings for the assistant and agents, read-only and summarised: the active findings worst first,
// with counts by severity, so a long account does not reach the model as a list cut off in the middle. A maintainer
// asked an agent for Inspector findings through call_aws, which is for administrators only and so is refused in a
// workflow, and whose raw output was cut off.

const (
	inspectorMaxItems = 1000 // findings read per call
	inspectorShowMax  = 80
)

var inspectorSeverities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "INFORMATIONAL", "UNTRIAGED"}

type inspectorFinding struct {
	Severity        string   `json:"severity"`
	Title           string   `json:"title"`
	Type            string   `json:"type"`
	FixAvailable    string   `json:"fixAvailable"`
	ExploitAvailble string   `json:"exploitAvailable"`
	InspectorScore  *float64 `json:"inspectorScore"`
	Epss            *struct {
		Score float64 `json:"score"`
	} `json:"epss"`
	Resources []struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Details struct {
			ECR *struct {
				Repository string   `json:"repositoryName"`
				Tags       []string `json:"imageTags"`
			} `json:"awsEcrContainerImage"`
			EC2 *struct {
				Platform string `json:"platform"`
			} `json:"awsEc2Instance"`
			Lambda *struct {
				Name string `json:"functionName"`
			} `json:"awsLambdaFunction"`
		} `json:"details"`
	} `json:"resources"`
	Package *struct {
		ID       string `json:"vulnerabilityId"`
		Packages []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Fixed   string `json:"fixedInVersion"`
		} `json:"vulnerablePackages"`
	} `json:"packageVulnerabilityDetails"`
}

func (f inspectorFinding) resource() string {
	if len(f.Resources) == 0 {
		return "an unnamed resource"
	}
	r := f.Resources[0]
	switch {
	case r.Details.ECR != nil:
		name := "ECR image " + r.Details.ECR.Repository
		if len(r.Details.ECR.Tags) > 0 {
			name += ":" + strings.Join(r.Details.ECR.Tags, ",")
		}
		return name
	case r.Details.Lambda != nil:
		return "Lambda function " + r.Details.Lambda.Name
	}
	id := r.ID
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	return strings.TrimPrefix(strings.ToLower(strings.ReplaceAll(r.Type, "_", " ")), "aws ") + " " + id
}

func (f inspectorFinding) line() string {
	var b strings.Builder
	fmt.Fprintf(&b, "- [%s] ", f.Severity)
	if f.Package != nil && f.Package.ID != "" {
		b.WriteString(f.Package.ID)
		for i, p := range f.Package.Packages {
			if i == 2 {
				fmt.Fprintf(&b, " and %d more packages", len(f.Package.Packages)-2)
				break
			}
			fmt.Fprintf(&b, " in %s %s", p.Name, p.Version)
			if p.Fixed != "" && p.Fixed != "NotAvailable" {
				fmt.Fprintf(&b, " (fixed in %s)", p.Fixed)
			}
		}
	} else {
		b.WriteString(truncate(f.Title, 140))
	}
	fmt.Fprintf(&b, " on %s", f.resource())
	var extra []string
	if f.FixAvailable != "" {
		extra = append(extra, "fix available: "+strings.ToLower(f.FixAvailable))
	}
	if f.ExploitAvailble != "" {
		extra = append(extra, "known exploit: "+strings.ToLower(f.ExploitAvailble))
	}
	if f.Epss != nil {
		extra = append(extra, fmt.Sprintf("EPSS %.3f", f.Epss.Score))
	}
	if f.InspectorScore != nil {
		extra = append(extra, fmt.Sprintf("score %.1f", *f.InspectorScore))
	}
	if len(extra) > 0 {
		b.WriteString(" · " + strings.Join(extra, " · "))
	}
	return b.String()
}

func (s *Server) lookupInspectorFindings(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Account  string `json:"account"`
		Severity string `json:"severity"`
		Limit    int    `json:"limit"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	if a.Account == "" {
		a.Account = primaryAccount
	}
	a.Severity = strings.ToUpper(strings.TrimSpace(a.Severity))
	if a.Severity != "" && !contains(inspectorSeverities, a.Severity) {
		return "", errors.New("severity: critical, high, medium, low or informational")
	}
	if a.Limit < 1 || a.Limit > inspectorShowMax {
		a.Limit = 40
	}
	off, err := s.awsServicesOff(ctx)
	if err != nil {
		return "", errors.New("the access policy could not be read, so Inspector findings are not shown until it can")
	}
	if off[svcOther] {
		return "", errors.New("an administrator turned off the AI's access to other AWS services, which includes Amazon Inspector")
	}
	st, err := s.awsFor(ctx, a.Account)
	if err != nil {
		return "", err
	}
	if !st.Enabled {
		return "", errors.New("AWS is not connected to this Forgeline server")
	}
	filter := map[string]any{"findingStatus": []map[string]string{{"comparison": "EQUALS", "value": "ACTIVE"}}}
	if a.Severity != "" {
		filter["severity"] = []map[string]string{{"comparison": "EQUALS", "value": a.Severity}}
	}
	fc, _ := json.Marshal(filter)
	out, err := s.aws(ctx, st, "inspector2", "list-findings", "--filter-criteria", string(fc),
		"--sort-criteria", `{"field":"SEVERITY","sortOrder":"DESC"}`, "--max-items", fmt.Sprint(inspectorMaxItems))
	if err != nil {
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "accessdenied") || strings.Contains(low, "not authorized") {
			return "", fmt.Errorf("the connection for %s may not read Amazon Inspector (it needs inspector2:ListFindings): %v", a.Account, err)
		}
		if strings.Contains(low, "validationexception") || strings.Contains(low, "not enabled") {
			return "", fmt.Errorf("Amazon Inspector is not turned on for %s in %s, so it has no findings to read", a.Account, st.Region)
		}
		return "", err
	}
	var resp struct {
		Findings []inspectorFinding `json:"findings"`
		Next     string             `json:"NextToken"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", errors.New("could not read what Amazon Inspector returned")
	}
	return formatInspector(a.Account, st.Region, a.Severity, a.Limit, resp.Findings, resp.Next != ""), nil
}

// formatInspector is the findings as the model reads them: counts first, then the worst, one line each.
func formatInspector(account, region, severity string, limit int, fs []inspectorFinding, more bool) string {
	rank := map[string]int{}
	for i, sev := range inspectorSeverities {
		rank[sev] = i
	}
	sort.SliceStable(fs, func(i, j int) bool {
		if rank[fs[i].Severity] != rank[fs[j].Severity] {
			return rank[fs[i].Severity] < rank[fs[j].Severity]
		}
		return fs[i].ExploitAvailble == "YES" && fs[j].ExploitAvailble != "YES"
	})
	count := map[string]int{}
	byType := map[string]int{}
	for _, f := range fs {
		count[f.Severity]++
		if len(f.Resources) > 0 {
			byType[strings.TrimPrefix(f.Resources[0].Type, "AWS_")]++
		}
	}
	var b strings.Builder
	scope := "active findings"
	if severity != "" {
		scope = "active " + strings.ToLower(severity) + " findings"
	}
	fmt.Fprintf(&b, "Amazon Inspector, account %s, region %s: %d %s", account, region, len(fs), scope)
	if more {
		fmt.Fprintf(&b, " (the first %d; there are more)", len(fs))
	}
	b.WriteString(".\nBy severity:")
	for _, sev := range inspectorSeverities {
		if count[sev] > 0 {
			fmt.Fprintf(&b, " %s %d,", strings.ToLower(sev), count[sev])
		}
	}
	text := strings.TrimSuffix(b.String(), ",")
	b.Reset()
	b.WriteString(text)
	if len(byType) > 0 {
		types := make([]string, 0, len(byType))
		for t, n := range byType {
			types = append(types, fmt.Sprintf("%s %d", strings.ToLower(strings.ReplaceAll(t, "_", " ")), n))
		}
		sort.Strings(types)
		b.WriteString(".\nBy resource type: " + strings.Join(types, ", "))
	}
	if len(fs) == 0 {
		return b.String() + ".\nNo active findings. That means Inspector found nothing open; check it scans the resources you expect."
	}
	fmt.Fprintf(&b, ".\nWorst first (showing %d):\n", min(limit, len(fs)))
	for i, f := range fs {
		if i == limit {
			fmt.Fprintf(&b, "%d more not shown; ask with a severity, or a smaller account, to see them.\n", len(fs)-limit)
			break
		}
		b.WriteString(f.line() + "\n")
	}
	return b.String()
}
