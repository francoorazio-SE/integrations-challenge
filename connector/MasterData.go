package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	helpers "github.com/fatjonblp/coding_challange_integrations/connector/helpers/go"
)

// Phase 1, master data : read suppliers, purchase orders and
// purchase order lines from the ERP's paginated REST surface and deliver them
// into twin

const tenant = "steinbach-ch10"

// twin's own cap per ingest chunk
const maxChunkRecords = 1000

// ERP Supplier response from /erp/v1/suppliers
type Supplier struct {
	SupplierNumber   string `json:"supplier_number"`
	Name             string `json:"name"`
	Country          string `json:"country"`
	Currency         string `json:"currency"`
	IBAN             string `json:"iban"`
	VATNumber        string `json:"vat_number"`
	PaymentTermsDays int    `json:"payment_terms_days"`
	Blocked          bool   `json:"blocked"`
	ChangeSeq        int64  `json:"change_seq"`
}

// ERP PO response from /erp/v1/purchase-orders
type PurchaseOrder struct {
	PONumber       string `json:"po_number"`
	SupplierNumber string `json:"supplier_number"`
	CompanyCode    string `json:"company_code"`
	Currency       string `json:"currency"`
	Status         string `json:"status"`
	OrderDate      string `json:"order_date"`
	CostCenter     string `json:"cost_center"`
	ChangeSeq      int64  `json:"change_seq"`
}

// ERP PO response from /erp/v1/purchase-orders-lines
type PurchaseOrderLine struct {
	PONumber    string `json:"po_number"`
	LineNo      string `json:"line_no"`
	Material    string `json:"material"`
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UoM         string `json:"uom"`
	UnitPrice   string `json:"unit_price"`
	Currency    string `json:"currency"`
	GLAccount   string `json:"gl_account"`
	CostCenter  string `json:"cost_center"`
	ChangeSeq   int64  `json:"change_seq"`
}

type listEnvelope[T any] struct {
	Records      []T    `json:"records"`
	NextCursor   string `json:"next_cursor"`
	MaxChangeSeq int64  `json:"max_change_seq"`
	Returned     int    `json:"returned"`
	HasMore      bool   `json:"has_more"`
}

// Paginated API requests for suppliers, PO, and PO lines with watermark ( valid only once has_more = false)
func fetchList[T any](erp *helpers.Client, path string, changedSince int64) ([]T, int64, error) {
	var all []T
	cursor := ""
	var maxSeq int64
	for {
		q := url.Values{}
		q.Set("limit", "250")
		if changedSince > 0 {
			q.Set("changed_since", strconv.FormatInt(changedSince, 10))
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		status, _, raw, err := erp.Do(http.MethodGet, path+"?"+q.Encode(), nil, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("GET %s: %w", path, err)
		}
		if status != http.StatusOK {
			return nil, 0, fmt.Errorf("GET %s: unexpected status %d: %s", path, status, raw)
		}
		var env listEnvelope[T]
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, 0, fmt.Errorf("GET %s: decode: %w", path, err)
		}
		all = append(all, env.Records...)
		if !env.HasMore {
			maxSeq = env.MaxChangeSeq
			break
		}
		cursor = env.NextCursor
	}
	return all, maxSeq, nil
}

// watermarks per type - to be used for fetching deltas
type watermarkState struct {
	Supplier          int64 `json:"supplier"`
	PurchaseOrder     int64 `json:"purchase_order"`
	PurchaseOrderLine int64 `json:"purchase_order_line"`
}

func watermarkPath(stateDir string) string {
	return filepath.Join(stateDir, "master_data_watermark.json")
}

func loadWatermark(stateDir string) (state watermarkState, existed bool, err error) {
	raw, err := os.ReadFile(watermarkPath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return watermarkState{}, false, nil
	}
	if err != nil {
		return watermarkState{}, false, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return watermarkState{}, false, fmt.Errorf("decode %s: %w", watermarkPath(stateDir), err)
	}
	return state, true, nil
}

// save wm on state dir
func saveWatermark(stateDir string, state watermarkState) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := watermarkPath(stateDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type manifestFile struct {
	Path     string `json:"path"`
	Dataset  string `json:"dataset"`
	Format   string `json:"format"`
	Profile  string `json:"profile"`
	Encoding string `json:"encoding"`
}

type manifest struct {
	ManifestVersion string         `json:"manifest_version"`
	BatchID         string         `json:"batch_id"`
	Producer        string         `json:"producer,omitempty"`
	RunID           string         `json:"run_id,omitempty"`
	Tenant          string         `json:"tenant,omitempty"`
	SourceSystem    string         `json:"source_system,omitempty"`
	Mode            string         `json:"mode,omitempty"`
	FullLoad        bool           `json:"full_load"`
	OnError         string         `json:"on_error,omitempty"`
	Files           []manifestFile `json:"files,omitempty"`
}

type openBatchResponse struct {
	BatchRef string `json:"batch_ref"`
	Status   string `json:"status"`
	Replay   bool   `json:"replay"`
}

type chunkCounts struct {
	Seen                int `json:"seen"`
	Accepted            int `json:"accepted"`
	AcceptedWithWarning int `json:"accepted_with_warning"`
	Rejected            int `json:"rejected"`
}

type chunkResponse struct {
	Counts chunkCounts `json:"counts"`
}

// Master Data Totals
type MasterDataResult struct {
	FullLoad bool
	Read     map[string]int
	Seen     map[string]int
	Accepted map[string]int
	Rejected map[string]int
}

func newMasterDataResult() MasterDataResult {
	return MasterDataResult{
		Read:     map[string]int{},
		Seen:     map[string]int{},
		Accepted: map[string]int{},
		Rejected: map[string]int{},
	}
}

// SyncMasterData - phase 1: pull suppliers, purchase orders and purchase order lines
func SyncMasterData(erp, twin *helpers.Client, runID, stateDir string) (MasterDataResult, error) {
	result := newMasterDataResult()

	wm, existed, err := loadWatermark(stateDir)
	if err != nil {
		return result, fmt.Errorf("load watermark: %w", err)
	}
	//it's a full load only if there are no watermarks
	result.FullLoad = !existed

	suppliers, supplierSeq, err := fetchList[Supplier](erp, "/erp/v1/suppliers", wm.Supplier)
	if err != nil {
		return result, fmt.Errorf("fetch suppliers: %w", err)
	}
	result.Read["supplier"] = len(suppliers)

	orders, orderSeq, err := fetchList[PurchaseOrder](erp, "/erp/v1/purchase-orders", wm.PurchaseOrder)
	if err != nil {
		return result, fmt.Errorf("fetch purchase orders: %w", err)
	}
	//order no. might contain empty spaces
	for i := range orders {
		orders[i].PONumber = strings.TrimSpace(orders[i].PONumber)
	}
	result.Read["purchase_order"] += len(orders)

	lines, lineSeq, err := fetchList[PurchaseOrderLine](erp, "/erp/v1/purchase-order-lines", wm.PurchaseOrderLine)
	if err != nil {
		return result, fmt.Errorf("fetch purchase order lines: %w", err)
	}
	for i := range lines {
		//order no. might contain empty spaces
		lines[i].PONumber = strings.TrimSpace(lines[i].PONumber)
	}
	result.Read["purchase_order_line"] += len(lines)

	//Deliver the result to twin if at least 1 of the results contains records
	if len(suppliers) > 0 || len(orders) > 0 || len(lines) > 0 {
		if err := deliverMasterData(twin, runID, result.FullLoad, suppliers, orders, lines, &result); err != nil {
			return result, err
		}
	}

	newWM := watermarkState{Supplier: supplierSeq, PurchaseOrder: orderSeq, PurchaseOrderLine: lineSeq}
	if err := saveWatermark(stateDir, newWM); err != nil {
		return result, fmt.Errorf("save watermark: %w", err)
	}
	return result, nil
}

func deliverMasterData(twin *helpers.Client, runID string, fullLoad bool,
	suppliers []Supplier, orders []PurchaseOrder, lines []PurchaseOrderLine,
	result *MasterDataResult) error {

	batchID := "master-" + runID
	m := manifest{
		ManifestVersion: "1",
		BatchID:         batchID,
		Producer:        "blp-connector/" + connectorVersion(),
		RunID:           runID,
		Tenant:          tenant,
		SourceSystem:    "erp-prod",
		Mode:            "upsert",
		FullLoad:        fullLoad,
		OnError:         "continue",
	}
	if len(suppliers) > 0 {
		m.Files = append(m.Files, manifestFile{Path: "supplier.csv", Dataset: "supplier", Format: "json", Profile: "blp-canonical-v1", Encoding: "UTF-8"})
	}
	if len(orders) > 0 {
		m.Files = append(m.Files, manifestFile{Path: "purchase_order.csv", Dataset: "purchase_order", Format: "json", Profile: "blp-canonical-v1", Encoding: "UTF-8"})
	}
	if len(lines) > 0 {
		m.Files = append(m.Files, manifestFile{Path: "purchase_order_line.csv", Dataset: "purchase_order_line", Format: "json", Profile: "blp-canonical-v1", Encoding: "UTF-8"})
	}

	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	status, _, raw, err := twin.Do(http.MethodPost, "/v1/ingest/batches", body, nil)
	if err != nil {
		return fmt.Errorf("open batch %s: %w", batchID, err)
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("open batch %s: unexpected status %d: %s", batchID, status, raw)
	}
	var opened openBatchResponse
	if err := json.Unmarshal(raw, &opened); err != nil {
		return fmt.Errorf("open batch %s: decode: %w", batchID, err)
	}

	if len(suppliers) > 0 {
		if err := sendDataset(twin, opened.BatchRef, "supplier", suppliers, result); err != nil {
			return err
		}
	}
	if len(orders) > 0 {
		if err := sendDataset(twin, opened.BatchRef, "purchase_order", orders, result); err != nil {
			return err
		}
	}
	if len(lines) > 0 {
		if err := sendDataset(twin, opened.BatchRef, "purchase_order_line", lines, result); err != nil {
			return err
		}
	}

	status, _, raw, err = twin.Do(http.MethodPost, "/v1/ingest/batches/"+opened.BatchRef+"/commit", []byte("{}"), nil)
	if err != nil {
		return fmt.Errorf("commit batch %s: %w", batchID, err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("commit batch %s: unexpected status %d: %s", batchID, status, raw)
	}
	return nil
}

func sendDataset[T any](twin *helpers.Client, batchRef, dataset string, records []T, result *MasterDataResult) error {
	for start, ordinal := 0, 1; start < len(records); start, ordinal = start+maxChunkRecords, ordinal+1 {
		end := start + maxChunkRecords
		if end > len(records) {
			end = len(records)
		}

		chunk := records[start:end]
		body, err := json.Marshal(chunk)
		if err != nil {
			return fmt.Errorf("marshal %s chunk %d: %w", dataset, ordinal, err)
		}

		headers := map[string]string{
			"Idempotency-Key": fmt.Sprintf("ing:%s:%s:%d", batchRef, dataset, ordinal),
			"X-Chunk-Ordinal": strconv.Itoa(ordinal),
		}

		path := fmt.Sprintf("/v1/ingest/batches/%s/records?dataset=%s", batchRef, dataset)
		status, _, raw, err := twin.Do(http.MethodPost, path, body, headers)
		if err != nil {
			return fmt.Errorf("%s chunk %d: %w", dataset, ordinal, err)
		}

		if status != http.StatusOK && status != http.StatusMultiStatus && status != http.StatusUnprocessableEntity {
			return fmt.Errorf("%s chunk %d: unexpected status %d: %s", dataset, ordinal, status, raw)
		}

		var resp chunkResponse

		if err := json.Unmarshal(raw, &resp); err != nil {
			return fmt.Errorf("%s chunk %d: decode: %w", dataset, ordinal, err)
		}

		result.Seen[dataset] += resp.Counts.Seen
		result.Accepted[dataset] += resp.Counts.Accepted + resp.Counts.AcceptedWithWarning
		result.Rejected[dataset] += resp.Counts.Rejected
	}
	return nil
}
