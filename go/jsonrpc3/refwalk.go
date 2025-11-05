package jsonrpc3

import (
	"encoding/json"
	"reflect"
)

// processResult walks through a result value using reflection and:
// 1. Finds Object implementations and registers them, replacing with Reference
// 2. Processes nested structures (maps, slices, structs)
// Returns the processed result ready for JSON marshaling.
func processResult(handler *Handler, result any) any {
	if result == nil {
		return nil
	}

	return walkValue(handler, reflect.ValueOf(result)).Interface()
}

// walkValue recursively processes a reflect.Value
func walkValue(handler *Handler, v reflect.Value) reflect.Value {
	// Handle invalid values
	if !v.IsValid() {
		return v
	}

	// Check if this value (before dereferencing) implements Object
	if v.CanInterface() {
		iface := v.Interface()

		// Check for Object interface (works with pointers)
		if obj, ok := iface.(Object); ok {
			// Generate ref ID and register the object with handler
			ref := handler.session.GenerateRefID()
			handler.session.AddLocalRef(ref, obj)
			return reflect.ValueOf(NewReference(ref))
		}

		// Check if it's already a Reference - leave it as-is
		if _, ok := iface.(Reference); ok {
			return v
		}

		// Check if it implements json.Marshaler - don't process it
		// This preserves custom JSON encoding (e.g., for BigInt, RegExp)
		if _, ok := iface.(json.Marshaler); ok {
			return v
		}
	}

	// Dereference pointers and interfaces for further processing
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return v
		}
		v = v.Elem()
	}

	// Process based on kind
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		return walkSlice(handler, v)

	case reflect.Map:
		return walkMap(handler, v)

	case reflect.Struct:
		return walkStruct(handler, v)

	default:
		// Primitive types, return as-is
		return v
	}
}

// walkSlice processes a slice or array
func walkSlice(handler *Handler, v reflect.Value) reflect.Value {
	length := v.Len()
	if length == 0 {
		return v
	}

	// Create a new slice or array of the same type
	var newSlice reflect.Value
	if v.Kind() == reflect.Array {
		// For arrays, we need to create a new array value
		newSlice = reflect.New(v.Type()).Elem()
	} else {
		// For slices, use MakeSlice
		newSlice = reflect.MakeSlice(v.Type(), length, length)
	}
	modified := false

	for i := 0; i < length; i++ {
		elem := v.Index(i)
		newElem := walkValue(handler, elem)

		if newElem.IsValid() && newElem.CanInterface() && elem.CanInterface() {
			// Check if the value was modified
			if !reflect.DeepEqual(elem.Interface(), newElem.Interface()) {
				modified = true
			}
		}

		if newElem.IsValid() && newElem.Type().AssignableTo(newSlice.Index(i).Type()) {
			newSlice.Index(i).Set(newElem)
		} else if elem.IsValid() {
			newSlice.Index(i).Set(elem)
		}
	}

	if modified {
		return newSlice
	}
	return v
}

// walkMap processes a map
func walkMap(handler *Handler, v reflect.Value) reflect.Value {
	if v.Len() == 0 {
		return v
	}

	// Create a new map of the same type
	newMap := reflect.MakeMap(v.Type())
	modified := false

	iter := v.MapRange()
	for iter.Next() {
		key := iter.Key()
		val := iter.Value()
		newVal := walkValue(handler, val)

		if newVal.IsValid() && newVal.CanInterface() && val.CanInterface() {
			// Check if the value was modified
			if !reflect.DeepEqual(val.Interface(), newVal.Interface()) {
				modified = true
			}
		}

		if newVal.IsValid() && newVal.Type().AssignableTo(v.Type().Elem()) {
			newMap.SetMapIndex(key, newVal)
		} else if val.IsValid() {
			newMap.SetMapIndex(key, val)
		}
	}

	if modified {
		return newMap
	}
	return v
}

// walkStruct processes a struct
func walkStruct(handler *Handler, v reflect.Value) reflect.Value {
	// Don't modify structs that we can't create (unexported)
	// For now, we'll just check fields but return original struct
	// In a more sophisticated implementation, we might clone the struct

	typ := v.Type()
	modified := false
	fields := make(map[int]reflect.Value)

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := typ.Field(i)

		// Skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		newField := walkValue(handler, field)
		if newField.IsValid() && newField.CanInterface() && field.CanInterface() {
			if !reflect.DeepEqual(field.Interface(), newField.Interface()) {
				modified = true
				fields[i] = newField
			}
		}
	}

	// If nothing changed, return original
	if !modified {
		return v
	}

	// Create a new struct with modified fields
	// This requires the struct to be addressable/settable
	newStruct := reflect.New(typ).Elem()
	for i := 0; i < v.NumField(); i++ {
		fieldType := typ.Field(i)
		if !fieldType.IsExported() {
			continue
		}

		if newVal, ok := fields[i]; ok {
			if newVal.Type().AssignableTo(newStruct.Field(i).Type()) {
				newStruct.Field(i).Set(newVal)
			}
		} else {
			newStruct.Field(i).Set(v.Field(i))
		}
	}

	return newStruct
}
