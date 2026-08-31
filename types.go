package mysql

import (
	"fmt"
	"github.com/SanjayDrop5528/models-go-engine/model"
	"github.com/SanjayDrop5528/models-go-engine/schema"
)

// ToMySQLType maps core generic DataType to MySQL SQL types.
func ToMySQLType(attr schema.SchemaAttribute) string {
	switch attr.Type {
	case model.TypeString:
		if attr.Length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", attr.Length)
		}
		return "VARCHAR(255)"

	case model.TypeText:
		return "TEXT"

	case model.TypeInt:
		return "INT"

	case model.TypeLong:
		return "BIGINT"

	case model.TypeFloat:
		return "DOUBLE"

	case model.TypeDecimal:
		if attr.Precision > 0 && attr.Scale > 0 {
			return fmt.Sprintf("DECIMAL(%d, %d)", attr.Precision, attr.Scale)
		}
		return "DECIMAL(10, 2)"

	case model.TypeBoolean:
		return "TINYINT(1)"

	case model.TypeDateTime:
		return "DATETIME"

	case model.TypeDate:
		return "DATE"

	case model.TypeTime:
		return "TIME"

	case model.TypeJSON:
		return "JSON"

	case model.TypeUUID:
		return "VARCHAR(36)"

	case model.TypeBinary:
		return "BLOB"

	default:
		return "VARCHAR(255)"
	}
}
