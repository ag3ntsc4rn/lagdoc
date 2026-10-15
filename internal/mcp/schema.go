package mcp

import "slices"

// JSON Schemas (2020-12) for the tools' inputs and structured outputs.
// The output schemas describe the --json documents exactly
// (additionalProperties false), and a test checks real output against
// them, so a new field can't ship undocumented.

func object(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// closed is an object whose properties are all required.
func closed(props map[string]any) map[string]any {
	req := make([]string, 0, len(props))
	for k := range props {
		req = append(req, k)
	}
	slices.Sort(req) // tools/list must be the same every time
	return object(props, req...)
}

func prop(typ, description string) map[string]any {
	return map[string]any{"type": typ, "description": description}
}

func typ(t any) map[string]any { return map[string]any{"type": t} }

func arrayOf(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func enum(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

var (
	str      = typ("string")
	integer  = typ("integer")
	number   = typ("number")
	boolean  = typ("boolean")
	strings_ = arrayOf(str)
	dateTime = map[string]any{"type": "string", "format": "date-time"}
	severity = enum("crit", "warn", "ok", "unknown")
	verdict  = enum("slow", "warn", "ok", "unknown")

	hostSchema = closed(map[string]any{
		"hostname": str, "os": str, "os_name": str, "uptime_seconds": integer, "cpus": integer,
	})

	findingSchema = object(map[string]any{
		"check":    str,
		"severity": severity,
		"summary":  str,
		"percent":  number, // left out when a check has no single percentage
		"details":  strings_,
	}, "check", "severity", "summary", "details")

	reportSchema = closed(map[string]any{
		"schema_version": integer,
		"taken_at":       dateTime,
		"host":           hostSchema,
		"verdict":        verdict,
		"causes":         integer,
		"findings":       arrayOf(findingSchema),
	})

	topSchema = closed(map[string]any{
		"schema_version": integer,
		"taken_at":       dateTime,
		"host":           hostSchema,
		"sort":           enum("memory", "cpu"),
		"apps": arrayOf(closed(map[string]any{
			"name": str, "processes": integer, "rss_bytes": integer, "cpu_percent": number,
		})),
	})

	startupItem = closed(map[string]any{"name": str, "source": str})
	app         = object(map[string]any{"name": str, "version": str, "source": str}, "name", "source")
	usage       = closed(map[string]any{"rss_bytes": integer, "cpu_percent": number})

	changesSchema = closed(map[string]any{
		"schema_version": integer,
		"taken_at":       dateTime,
		"since":          map[string]any{"type": []string{"string", "null"}, "format": "date-time"},
		"host":           hostSchema,
		"changes":        integer,
		"startup_items":  closed(map[string]any{"added": arrayOf(startupItem), "removed": arrayOf(startupItem)}),
		"apps": closed(map[string]any{
			"added":   arrayOf(app),
			"removed": arrayOf(app),
			"updated": arrayOf(closed(map[string]any{"name": str, "from": str, "to": str})),
		}),
		"disks": arrayOf(closed(map[string]any{
			"mount": str, "used_change_bytes": integer, "free_bytes": integer, "total_bytes": integer,
		})),
		"heavy_apps": arrayOf(closed(map[string]any{
			"name":      str,
			"processes": integer,
			"now":       usage,
			"before":    map[string]any{"anyOf": []any{usage, typ("null")}},
			"reasons":   arrayOf(enum("memory", "cpu")),
		})),
		"unread":  strings_,
		"skipped": strings_,
		"saved":   boolean,
	})

	fixSchema = closed(map[string]any{
		"schema_version": integer,
		"taken_at":       dateTime,
		"host":           hostSchema,
		"verdict":        verdict,
		"fixes": arrayOf(closed(map[string]any{
			"id": str, "check": str, "title": str, "note": str, "manual": boolean,
			"command": str, "steps": strings_, "frees_bytes": integer, "mount": str,
		})),
	})
)
