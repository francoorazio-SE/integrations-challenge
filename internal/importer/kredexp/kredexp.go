// Package kredexp reads the legacy Kreditoren-Sammelexport of Steinbach
// Industrie AG, format version 2.1.
//
// THIS PACKAGE IS A STUB. Implementing it is the Go task.
//
// The specification is the customer's own interface document,
// docs/customer/KRED-EXP-2.1.md. That document, and not this comment, is the
// contract: it describes a CP1252 file with no byte order mark, semicolon
// separated, CRLF terminated, with doubled quotes and embedded line breaks in
// quoted fields, the record types VORLAUF, KOPF, POS and NACHLAUF, decimal
// points with optional apostrophe grouping, negative amounts carrying a trailing
// minus, six-digit TTMMJJ dates with a pivot at 70, significant leading zeros
// everywhere, and a trailer with record counts and control totals that the
// receiver has to verify.
//
// It also contains three genuine contradictions. They are not typographical
// mistakes to be tidied up; they are the interesting part. Read
// internal/importer/kredexp/README.md for the harness contract, and read section
// 9 of the customer document, "Open points and known deviations", twice.
//
// What this package must satisfy is [importer.Format]. What it must not do is
// return an error for a defect in the data: a malformed line, a missing trailer
// and an undecodable byte are each a [importer.Diagnostic] on the
// [importer.Result], and the error return is reserved for a nil context and
// other programming faults. The pipeline is built on that distinction, and the
// golden runner asserts it.
package kredexp

import (
	"context"
	"strconv"
	"time"

	"github.com/fatjonblp/coding_challange_integrations/internal/importer"
	"github.com/fatjonblp/coding_challange_integrations/internal/model"
)

const OpenQuote = byte(0x22)
const CR = byte(0x0D)
const LF = byte(0x0A)

// ID is the format id, as it appears in a manifest's profile field and in a
// [importer.Result]. It is fixed: the grader looks the format up by this string.
const ID = "kredexp-2.1"

// FormatVersion is the only format version this importer accepts, from field 2
// of the Vorlaufsatz. A file declaring anything else is
// format_version_unsupported: section 9.6 of the customer document says v2.0
// exports are out of scope, and a v2.0 Positionssatz has ten fields rather than
// eleven, so parsing one as 2.1 would shift every field after the sixth.
const FormatVersion = "2.1"

// The diagnostic codes of this format, from BUILD-SPEC 10. The set is closed:
// the graded fixtures use only these, and a code outside the list cannot be
// graded because it is not published.
//
// Fatal codes are file level and the first one found wins, checked in the order
// they are declared here. Reject codes are record level and parsing continues
// past them. The trailer codes apply only when nothing was rejected, because a
// control total computed over an incomplete set of records tells you nothing;
// when something was rejected, the trailer is reported as unverified instead.
const (
	// CodeEncodingBOMPresent reports a byte order mark. The customer's general
	// format rules say "Windows-1252 (CP1252). No byte order mark." A BOM means
	// the file was written by something other than the agreed exporter, and the
	// first field of the first record is no longer "VORLAUF".
	CodeEncodingBOMPresent = "encoding_bom_present"
	// CodeEncodingInvalidByte reports a byte CP1252 does not define. Five
	// positions in the C1 range are undefined; see [importer.CP1252Rune].
	CodeEncodingInvalidByte = "encoding_invalid_byte"
	// CodeMalformedQuoting reports quoting the file never resolves: an
	// unterminated quoted field, or a quote where the grammar has none.
	CodeMalformedQuoting = "malformed_quoting"
	// CodeVorlaufMissing reports a file whose first record is not a Vorlaufsatz.
	CodeVorlaufMissing = "vorlauf_missing"
	// CodeFormatVersionUnsupported reports a Formatversion other than
	// [FormatVersion].
	CodeFormatVersionUnsupported = "format_version_unsupported"
	// CodeTrailerMissing reports a file with no Nachlaufsatz as its last
	// record. Section 7: a file whose control totals do not match must not be
	// posted, and a file with no totals at all has not been checked.
	CodeTrailerMissing = "trailer_missing"

	// CodeUnknownRecordType reports a Satzart outside VORLAUF, KOPF, POS and
	// NACHLAUF.
	CodeUnknownRecordType = "unknown_record_type"
	// CodeFieldCountMismatch reports a record with the wrong number of fields
	// for its Satzart.
	CodeFieldCountMismatch = "field_count_mismatch"
	// CodeMissingRequiredField reports an empty field the customer's field table
	// marks M.
	CodeMissingRequiredField = "missing_required_field"
	// CodeInvalidDate reports a date that is not six digits, or that names a day
	// the calendar does not have.
	CodeInvalidDate = "invalid_date"
	// CodeInvalidAmount reports an amount outside the accepted numeric shapes,
	// or one carrying more fraction digits than its field allows. An input is
	// never rounded: see [importer.ParseAmount].
	CodeInvalidAmount = "invalid_amount"
	// CodeOrphanLine reports a Positionssatz with no preceding Kopfsatz, or one
	// whose Belegnummer does not match the Kopfsatz it follows.
	CodeOrphanLine = "orphan_line"

	// CodeTrailerCountMismatch reports a declared record count that disagrees
	// with the number of records read.
	CodeTrailerCountMismatch = importer.CodeTrailerCountMismatch
	// CodeTrailerSumMismatch reports a declared control total that disagrees
	// with the sum of the records read.
	CodeTrailerSumMismatch = importer.CodeTrailerSumMismatch
	// CodeTrailerNotVerified reports that the trailer could not be checked
	// because a record was rejected.
	CodeTrailerNotVerified = importer.CodeTrailerNotVerified

	// CodeAmbiguousFieldSemantics reports a field whose meaning the customer's
	// own document leaves undecided. There are two such fields in this format
	// and section 9 of the customer document names both. Surfacing them is
	// correct; resolving them silently is not.
	CodeAmbiguousFieldSemantics = importer.CodeAmbiguousFieldSemantics
	// CodeDiagnosticsTruncated is appended by the framework when a file produces
	// more than [importer.MaxDiagnostics] findings. An implementation never
	// emits it itself: use [importer.Builder], which does.
	CodeDiagnosticsTruncated = importer.CodeDiagnosticsTruncated
)

// CodeNotImplemented is the diagnostic this stub reports. It is not part of the
// published code set and no fixture expects it: it exists so that a twin running
// an unimplemented importer degrades visibly - one fatal finding, an accepted
// file count of zero, a receipt that says why - instead of panicking, silently
// accepting nothing, or crashing a batch that also carries canonical files.
const CodeNotImplemented = "not_implemented"

const AmountTotalPosition = 9

// format is the KRED-EXP 2.1 importer.
type format struct{}

// New returns the KRED-EXP 2.1 importer.
func New() importer.Format { return format{} }

// init registers the format. The twin reaches it by blank-importing
// internal/importer/all, which is where the one line that links this package in
// belongs.
func init() { importer.Register(New()) }

// ID returns [ID].
func (format) ID() string { return ID }

// Detect reports whether head looks like a KRED-EXP 2.1 export.
//
// NOT IMPLEMENTED. It returns false, so the twin falls back on the manifest's
// declared profile and a batch that names kredexp-2.1 still reaches Parse.
//
// A real implementation sniffs the first record: the file has no header row and
// every record begins with its Satzart, so a Vorlaufsatz is a recognizable
// signature. It must stay false for the three canonical profiles' files, which
// the golden runner asserts, and it must not parse the file to decide.
func (format) Detect(head []byte, opt importer.Options) bool {
	file_content := string(head)
	if file_content[0:7] == "VORLAUF" || file_content[0:4] == "KOPF" || file_content[0:3] == "POS" || file_content[0:8] == "NACHLAUF" {
		return true
	}
	return false
}

type Record struct {
	value   []byte
	line    int
	ordinal int
}

var RecordType map[string]int = map[string]int{"VORLAUF": 8, "KOPF": 16, "POS": 11, "NACHLAUF": 5}

type FieldSpec struct {
	name      string
	required  bool
	date      bool
	fieldType FieldType
	amount    *importer.NumericFormat
}

type FieldType string

const (
	FieldTypeString   FieldType = "string"
	FieldTypeInt      FieldType = "int"
	FieldTypeDate     FieldType = "date"
	FieldTypeAmount   FieldType = "amount"
	FieldTypeDecimal  FieldType = "decimal"
	FieldTypeTime     FieldType = "time"
	FieldTypeDateTime FieldType = "datetime"
)

type total struct {
	InvoiceNo string
	AnzahlKop int
	AnzahlPos int
	SumBrutto model.Decimal
	SumPos    model.Decimal
}

var (
	amount2  = &importer.NumericFormat{MaxFractionDigits: 2}
	amount4  = &importer.NumericFormat{MaxFractionDigits: 4}
	qty3     = &importer.NumericFormat{MaxFractionDigits: 3}
	anyScale = func() *importer.NumericFormat { f := importer.NumericFormat{}.AnyScale(); return &f }()
	integer  = &importer.NumericFormat{MaxFractionDigits: 0}
)

var vorlaufFields = []FieldSpec{
	{"Satzart", true, false, FieldTypeString, nil}, {"Formatversion", true, false, FieldTypeString, nil}, {"Mandant", true, false, FieldTypeString, nil},
	{"Erstellungszeitpunkt", true, false, FieldTypeDateTime, nil}, {"Waehrung", true, false, FieldTypeString, nil}, {"Absender", true, false, FieldTypeString, nil},
	{"Empfaenger", true, false, FieldTypeString, nil}, {"Testkennzeichen", true, false, FieldTypeString, nil},
}

var kopfFields = []FieldSpec{
	{"Satzart", true, false, FieldTypeString, nil}, {"Belegnummer", true, false, FieldTypeString, nil},
	{"Lieferantennummer", true, false, FieldTypeString, nil}, {"Belegart", true, false, FieldTypeString, nil},
	{"Belegdatum", true, true, FieldTypeDate, nil}, {"Eingangsdatum", false, true, FieldTypeDate, nil},
	{"Bestellnummer", false, false, FieldTypeString, nil}, {"Waehrung", false, false, FieldTypeString, nil},
	{"Bruttobetrag", true, false, FieldTypeDecimal, amount2}, {"MWST-Betrag", false, false, FieldTypeDecimal, amount2},
	{"MWST-Code", false, false, FieldTypeString, nil}, {"Zahlungsziel", false, false, FieldTypeInt, integer},
	{"Skonto", false, false, FieldTypeDecimal, anyScale}, {"Skontotage", false, false, FieldTypeInt, integer},
	{"Kostenstelle", false, false, FieldTypeString, nil}, {"Text", false, false, FieldTypeString, nil},
}

var posFields = []FieldSpec{
	{"Satzart", true, false, FieldTypeString, nil}, {"Belegnummer", true, false, FieldTypeString, nil}, {"Positionsnummer", true, false, FieldTypeInt, integer},
	{"Sachkonto", true, false, FieldTypeString, nil}, {"Kostenstelle", false, false, FieldTypeString, nil}, {"Menge", true, false, FieldTypeDecimal, qty3},
	{"Einheit", true, false, FieldTypeString, nil}, {"Einzelpreis", true, false, FieldTypeDecimal, amount4}, {"Positionsbetrag", true, false, FieldTypeDecimal, amount2},
	{"Steuercode", false, false, FieldTypeString, nil}, {"Text", false, false, FieldTypeString, nil},
}

var nachlaufFields = []FieldSpec{
	{"Satzart", true, false, FieldTypeString, nil}, {"AnzahlKopfsaetze", true, false, FieldTypeInt, integer}, {"AnzahlPositionssaetze", true, false, FieldTypeInt, integer},
	{"SummeBruttobetrag", true, false, FieldTypeDecimal, amount2}, {"SummePositionsbetrag", true, false, FieldTypeDecimal, amount2},
}

// Parse reads raw and returns the invoices it contains.
//
// NOT IMPLEMENTED. It returns a [importer.Result] carrying one fatal
// [CodeNotImplemented] diagnostic and no documents, which is a well-formed
// answer: Accepted is false, the pipeline reports the file as rejected with a
// reason, and nothing downstream has to special-case an unfinished importer.
//
// The error return stays reserved for programming and environment faults. A nil
// context is one, and this stub already reports it, because the contract holds
// from the first line of the implementation and not from the last.
func (format) Parse(ctx context.Context, raw []byte, opt importer.Options) (*importer.Result, error) {
	if ctx == nil {
		return nil, importer.ErrNilContext
	}
	b := importer.NewBuilder(ID, opt)
	b.SetSourceBytes(raw)
	_, bom_exists := importer.StripBOM(raw)
	if bom_exists {
		b.Add(importer.Diagnostic{
			Code:     CodeEncodingBOMPresent,
			Severity: importer.SeverityFatal,
			Message:  "The code encoding contains BOM " + string(raw[0:2]),
		})
		return b.Result(), nil
	}
	var is_open_quote = false
	var cur_record []byte
	var record []Record
	var lineNo int
	var ordinal int
	var tot total
	for pos, by := range raw {
		//file level checks
		//Verify Coding per byte
		e, ok := importer.CP1252Rune(by)
		if !ok {
			b.Add(importer.Diagnostic{
				Code:     CodeEncodingInvalidByte,
				Severity: importer.SeverityFatal,
				Message:  "Encoding Invalid Byte: " + string(e),
			})
			return b.Result(), nil
		}
		//buil record, split only when CRLF is outside open quotes
		cur_record = append(cur_record, by)
		if by == OpenQuote {
			if is_open_quote {
				is_open_quote = false
			} else {
				is_open_quote = true
			}
		}
		if pos > 0 && raw[pos-1] == CR && by == LF {
			lineNo += 1
		}
		//create record if curr byte is LF and previuos one is CR outside open quotes
		if pos > 0 && raw[pos-1] == CR && by == LF && (!is_open_quote) {
			record = append(record, Record{value: cur_record[0 : len(cur_record)-2], line: lineNo, ordinal: ordinal})
			cur_record = nil
		}
	}
	if is_open_quote {
		b.Add(importer.Diagnostic{
			Code:     CodeMalformedQuoting,
			Severity: importer.SeverityFatal,
			Message:  "Malformed quoting",
		})
		return b.Result(), nil
	}
	if len(record) == 0 {
		b.Add(importer.Diagnostic{
			Code:     CodeVorlaufMissing,
			Severity: importer.SeverityFatal,
			Message:  "the file has no records",
		})
		return b.Result(), nil
	}
	fields, _ := splitFields(record[0].value)
	if fields[0] != "VORLAUF" {
		b.Add(importer.Diagnostic{
			Code:     CodeVorlaufMissing,
			Severity: importer.SeverityFatal,
			Message:  "VORLAUF is not the first record",
		})
		return b.Result(), nil
	}

	fields, _ = splitFields(record[len(record)-1].value)
	if fields[0] != "NACHLAUF" {
		b.Add(importer.Diagnostic{
			Code:     CodeTrailerMissing,
			Severity: importer.SeverityFatal,
			Message:  "The last record is not the expected trailer NACHLAUF",
		})
		return b.Result(), nil
	}
	var mandant, defaultCurrency string
	var current *model.APInvoice
	var currentLine int
	var computedLines int
	flush := func() {
		if current == nil {
			return
		}
		payload, _ := model.CanonicalJSON(*current)
		b.AddDocument(importer.Document{
			Dataset: string(model.DatasetInvoice),
			Key:     current.Key(),
			Line:    currentLine,
			Payload: payload,
		})
		current = nil
	}

	//line level
	for line, r := range record {
		fields, _ := splitFields(r.value)
		spec, known := FieldSpecFor(fields[0])
		if !known {
			b.Add(importer.Diagnostic{
				Code:     CodeUnknownRecordType,
				Severity: importer.SeverityReject,
				Line:     r.line,
				Record:   r.ordinal,
				Message:  "Unknown record type:" + fields[0],
			})
			continue
		}
		if len(spec) != len(fields) {
			b.Add(importer.Diagnostic{
				Code:     CodeFieldCountMismatch,
				Severity: importer.SeverityReject,
				Line:     r.line,
				Record:   r.ordinal,
				Message:  "Field count mistmatch, counted fields: " + strconv.Itoa(len(fields)) + " required fields: " + strconv.Itoa(RecordType[fields[0]]),
			})
			continue
		}
		//Increase File Total
		increaseFileTotal(&tot, fields[0])
		_, rejectBefore, _ := b.Counts()
		checkRequiredFields(b, spec, fields, line)

		for i, field := range fields {
			//chech date fields formatting
			if spec[i].date && field != "" {
				if !isValidDate(field) {
					b.Add(importer.Diagnostic{
						Code:     CodeInvalidDate,
						Severity: importer.SeverityReject,
						Line:     r.line,
						Record:   r.ordinal,
						Field:    spec[i].name,
						Value:    field,
						Message:  "Invalid date:" + field,
					})
					continue
				}
			}
			//Check decimal/int fields
			if spec[i].amount != nil && field != "" {
				amt, _, err := importer.ParseAmount(field, *spec[i].amount)
				if err != nil {
					b.Add(importer.Diagnostic{
						Code:     CodeInvalidAmount,
						Severity: importer.SeverityReject,
						Line:     r.line,
						Record:   r.ordinal,
						Field:    spec[i].name,
						Value:    field,
						Message:  spec[i].name + " is not a valid " + string(spec[i].fieldType),
					})
					continue
				}

				//Increase total by record type
				if fields[0] == "KOPF" && i+1 == AmountTotalPosition {
					if sum, err := tot.SumBrutto.Add(amt); err == nil {
						tot.SumBrutto = sum
					}
				}
				if fields[0] == "POS" && i+1 == AmountTotalPosition {
					if sum, err := tot.SumPos.Add(amt); err == nil {
						tot.SumPos = sum
					}
				}
			}
		}
		_, rejectAfter, _ := b.Counts()
		recordOK := rejectAfter == rejectBefore

		switch fields[0] {
		case "VORLAUF":
			mandant = fields[2]
			defaultCurrency = fields[4]

		case "KOPF":
			flush()
			if !recordOK {
				continue
			}
			docDate, _ := parseTTMMJJ(fields[4])
			recDate := ""
			if fields[5] != "" {
				recDate, _ = parseTTMMJJ(fields[5])
			}
			currency := fields[7]
			if currency == "" {
				currency = defaultCurrency
			}
			gross, _, _ := importer.ParseAmount(fields[8], *amount2)
			vat, _, _ := importer.ParseAmount(orDefault(fields[9], "0.00"), *amount2)
			paymentTerms, _ := importer.ParseInteger(orDefault(fields[11], "0"))
			discountDays, _ := importer.ParseInteger(orDefault(fields[13], "0"))
			current = &model.APInvoice{
				SupplierNumber:        fields[2],
				SupplierInvoiceNumber: fields[1],
				CompanyCode:           mandant,
				DocumentType:          fields[3],
				DocumentDate:          docDate,
				ReceiptDate:           recDate,
				PONumber:              fields[6],
				Currency:              currency,
				GrossAmount:           gross,
				VATAmount:             vat,
				VATCode:               fields[10],
				PaymentTermsDays:      paymentTerms,
				DiscountRaw:           fields[12],
				DiscountDays:          discountDays,
				CostCenter:            fields[14],
				Text:                  fields[15],
			}
			currentLine = r.line
			// Skonto
			if fields[12] != "" {
				b.Add(importer.Diagnostic{
					Code:     CodeAmbiguousFieldSemantics,
					Severity: importer.SeverityWarn,
					Line:     r.line,
					Record:   r.ordinal,
					Field:    "Skonto",
					Value:    fields[12],
					Message:  "Skonto's meaning (amount or percentage) is not defined by the customer document; carried verbatim",
				})
			}

		case "POS":
			if !recordOK {
				continue
			}
			if current == nil || current.SupplierInvoiceNumber != fields[1] {
				b.Add(importer.Diagnostic{
					Code:     CodeOrphanLine,
					Severity: importer.SeverityReject,
					Line:     r.line,
					Record:   r.ordinal,
					Field:    "Belegnummer",
					Value:    fields[1],
					Message:  "POS does not follow a matching KOPF",
				})
				continue
			}
			qty, _, _ := importer.ParseAmount(fields[5], *qty3)
			price, _, _ := importer.ParseAmount(fields[7], *amount4)
			lineAmt, _, _ := importer.ParseAmount(fields[8], *amount2)
			taxCode := fields[9]
			if taxCode == "" {
				taxCode = current.VATCode
			}
			if fields[4] == "" {
				// empty Kostenstelle =  the Kopfsatz's"
				b.Add(importer.Diagnostic{
					Code:     CodeAmbiguousFieldSemantics,
					Severity: importer.SeverityWarn,
					Line:     r.line,
					Record:   r.ordinal,
					Field:    "Kostenstelle",
					Message:  "an empty Kostenstelle is not defined as inherited or absent by the customer document; left empty",
				})
			}
			current.Lines = append(current.Lines, model.APInvoiceLine{
				LineNo:     fields[2],
				GLAccount:  fields[3],
				CostCenter: fields[4],
				Quantity:   qty,
				UoM:        fields[6],
				UnitPrice:  price,
				LineAmount: lineAmt,
				TaxCode:    taxCode,
			})
			computedLines++

		case "NACHLAUF":
			flush()
			declaredDocs, _ := importer.ParseInteger(fields[1])
			declaredLines, _ := importer.ParseInteger(fields[2])
			declaredGross, _, _ := importer.ParseAmount(fields[3], *amount2)
			declaredLineSum, _, _ := importer.ParseAmount(fields[4], *amount2)

			totals := importer.Totals{
				DeclaredDocuments: declaredDocs,
				DeclaredLines:     declaredLines,
				DeclaredGross:     declaredGross.String(),
				DeclaredLineSum:   declaredLineSum.String(),
				ComputedGross:     tot.SumBrutto.String(),
				ComputedLineSum:   tot.SumPos.String(),
				ComputedLines:     computedLines,
				TrailerDeclared:   true,
			}

			if !b.HasBlocking() {
				totals.TrailerVerified = declaredDocs == tot.AnzahlKop &&
					declaredLines == tot.AnzahlPos &&
					totals.DeclaredGross == totals.ComputedGross &&
					totals.DeclaredLineSum == totals.ComputedLineSum
			}
			b.SetTotals(totals)

			switch {
			case b.HasBlocking():
				b.Add(importer.Diagnostic{
					Code:     CodeTrailerNotVerified,
					Severity: importer.SeverityWarn,
					Line:     r.line,
					Record:   r.ordinal,
					Message:  "a record was rejected, so the trailer's control totals cannot be verified",
				})
			case !totals.TrailerVerified:
				if declaredDocs != tot.AnzahlKop || declaredLines != tot.AnzahlPos {
					b.Add(importer.Diagnostic{
						Code:     CodeTrailerCountMismatch,
						Severity: importer.SeverityWarn,
						Line:     r.line,
						Record:   r.ordinal,
						Message:  "the declared record counts do not match the number of records read",
					})
				}
				if totals.DeclaredGross != totals.ComputedGross || totals.DeclaredLineSum != totals.ComputedLineSum {
					b.Add(importer.Diagnostic{
						Code:     CodeTrailerSumMismatch,
						Severity: importer.SeverityWarn,
						Line:     r.line,
						Record:   r.ordinal,
						Message:  "the declared control totals do not match the sums read",
					})
				}
			}
		}
	}

	return b.Result(), nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func splitFields(rec []byte) (fields []string, ok bool) {
	var cur []rune
	inQuotes := false
	quoted := false

	for i := 0; i < len(rec); i++ {
		b := rec[i]

		if inQuotes {
			if b != OpenQuote {
				r, _ := importer.CP1252Rune(b)
				cur = append(cur, r)
				continue
			}
			if i+1 < len(rec) && rec[i+1] == OpenQuote {
				cur = append(cur, '"')
				i++
				continue
			}
			inQuotes = false
			continue
		}

		switch b {
		case ';':
			fields = append(fields, string(cur))
			cur = nil
			quoted = false
		case OpenQuote:
			if len(cur) > 0 || quoted {
				return nil, false
			}
			inQuotes = true
			quoted = true
		default:
			r, _ := importer.CP1252Rune(b)
			cur = append(cur, r)
		}
	}
	fields = append(fields, string(cur))
	return fields, true
}

func checkRequiredFields(b *importer.Builder, spec []FieldSpec, fields []string, line int) {
	for i, f := range spec {
		if f.required && fields[i] == "" {
			b.Add(importer.Diagnostic{
				Code:     CodeMissingRequiredField,
				Severity: importer.SeverityReject,
				Line:     line,
				Field:    f.name,
				Message:  f.name + " is required and was empty",
			})
		}
	}
}

func FieldSpecFor(recordType string) ([]FieldSpec, bool) {
	switch recordType {
	case "VORLAUF":
		return vorlaufFields, true
	case "KOPF":
		return kopfFields, true
	case "POS":
		return posFields, true
	case "NACHLAUF":
		return nachlaufFields, true
	}
	return nil, false
}

func isValidDate(value string) bool {
	if len(value) != 6 {
		return false
	}
	//check that chars are digits
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}

	day, _ := strconv.Atoi(value[0:2])
	month, _ := strconv.Atoi(value[2:4])
	year, _ := strconv.Atoi(value[4:6])

	if year >= 70 {
		year += 1900
	} else {
		year += 2000
	}

	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

	return date.Day() == day &&
		date.Month() == time.Month(month) &&
		date.Year() == year
}

func parseTTMMJJ(value string) (canonical string, ok bool) {
	if !isValidDate(value) {
		return "", false
	}
	day, month, yy := value[0:2], value[2:4], value[4:6]
	year := "20" + yy
	if yy[0] >= '7' {
		year = "19" + yy
	}
	return year + "-" + month + "-" + day, true
}

func increaseFileTotal(t *total, satzart string) {
	switch satzart {
	case "KOPF":
		t.AnzahlKop += 1
	case "POS":
		t.AnzahlPos += 1
	}
}
