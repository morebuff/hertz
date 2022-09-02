/*
 * Copyright 2022 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package protobuf

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudwego/hertz/cmd/hz/internal/generator"
	"github.com/cloudwego/hertz/cmd/hz/internal/generator/model"
	"github.com/cloudwego/hertz/cmd/hz/internal/protobuf/api"
	"github.com/cloudwego/hertz/cmd/hz/internal/util"
	"github.com/cloudwego/hertz/cmd/hz/internal/util/logs"
	"github.com/jhump/protoreflect/desc"
	"google.golang.org/protobuf/types/descriptorpb"
)

var BaseProto = descriptorpb.FileDescriptorProto{}

// getGoPackage  get option go_package
func getGoPackage(f *descriptorpb.FileDescriptorProto, pkgMap map[string]string) string {
	if f.Options == nil {
		f.Options = new(descriptorpb.FileOptions)
	}
	if f.Options.GoPackage == nil {
		f.Options.GoPackage = new(string)
	}
	goPkg := *f.Options.GoPackage

	// if go_package has ";", for example go_package="/a/b/c;d", we will use "/a/b/c" as go_package
	if strings.Contains(goPkg, ";") {
		pkg := strings.Split(goPkg, ";")
		if len(pkg) == 2 {
			logs.Warnf("The go_package of the file(%s) is \"%s\", hz will use \"%s\" as the go_package.", f.GetName(), goPkg, pkg[0])
			goPkg = pkg[0]
		}

	}

	if goPkg == "" {
		goPkg = f.GetPackage()
	}
	if opt, ok := pkgMap[f.GetName()]; ok {
		return opt
	}
	return goPkg
}

func switchBaseType(typ descriptorpb.FieldDescriptorProto_Type) *model.Type {
	switch typ {
	case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, descriptorpb.FieldDescriptorProto_TYPE_GROUP:
		return nil
	case descriptorpb.FieldDescriptorProto_TYPE_INT64:
		return model.TypeInt64
	case descriptorpb.FieldDescriptorProto_TYPE_INT32:
		return model.TypeInt32
	case descriptorpb.FieldDescriptorProto_TYPE_UINT64:
		return model.TypeUint64
	case descriptorpb.FieldDescriptorProto_TYPE_UINT32:
		return model.TypeUint32
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED64:
		return model.TypeUint64
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED32:
		return model.TypeUint32
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		return model.TypeBool
	case descriptorpb.FieldDescriptorProto_TYPE_STRING:
		return model.TypeString
	case descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		return model.TypeBinary
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED32:
		return model.TypeInt32
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
		return model.TypeInt64
	case descriptorpb.FieldDescriptorProto_TYPE_SINT32:
		return model.TypeInt32
	case descriptorpb.FieldDescriptorProto_TYPE_SINT64:
		return model.TypeInt64
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE:
		return model.TypeFloat64
	case descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
		return model.TypeFloat32
	}
	return nil
}

func astToService(ast *descriptorpb.FileDescriptorProto, resolver *Resolver) ([]*generator.Service, error) {
	resolver.ExportReferred(true, false)
	ss := ast.GetService()
	out := make([]*generator.Service, 0, len(ss))
	var merges model.Models

	for _, s := range ss {
		service := &generator.Service{
			Name: s.GetName(),
		}

		ms := s.GetMethod()
		methods := make([]*generator.HttpMethod, 0, len(ms))
		for _, m := range ms {

			hmethod, vpath := checkFirstOptions(HttpMethodOptions, m.GetOptions())
			if hmethod == "" {
				continue
			}
			path := vpath.(string)

			var handlerOutDir string
			genPath := checkFirstOption(api.E_HandlerPath, m.GetOptions())
			handlerOutDir, ok := genPath.(string)
			if !ok || len(handlerOutDir) == 0{
				handlerOutDir = ""
			}

			reqName := m.GetInputType()
			sb, err := resolver.ResolveIdentifier(reqName)
			reqName = util.BaseName(sb.Scope.GetOptions().GetGoPackage(), "") + "." + sb.Name
			if err != nil {
				return nil, err
			}
			respName := m.GetOutputType()
			st, err := resolver.ResolveIdentifier(respName)
			respName = util.BaseName(st.Scope.GetOptions().GetGoPackage(), "") + "." + st.Name
			if err != nil {
				return nil, err
			}

			var serializer string
			sl, sv := checkFirstOptions(SerializerOptions, m.GetOptions())
			if sl != "" {
				serializer = sv.(string)
			}

			method := &generator.HttpMethod{
				Name:       util.CamelString(m.GetName()),
				HTTPMethod: hmethod,
				Path:       path,
				Serializer: serializer,
				OutputDir:  handlerOutDir,
			}

			goOptMapAlias := make(map[string]string, 1)
			refs := resolver.ExportReferred(false, true)
			method.Models = make(map[string]*model.Model, len(refs))
			for _, ref := range refs {
				if val, exist := method.Models[ref.Model.PackageName]; exist {
					if val.Package == ref.Model.Package {
						method.Models[ref.Model.PackageName] = ref.Model
						goOptMapAlias[ref.Model.Package] = ref.Model.PackageName
					} else {
						file := filepath.Base(ref.Model.FilePath)
						fileName := strings.Split(file, ".")
						newPkg := fileName[len(fileName)-2] + "_" + val.PackageName
						method.Models[newPkg] = ref.Model
						goOptMapAlias[ref.Model.Package] = newPkg
					}
					continue
				}
				method.Models[ref.Model.PackageName] = ref.Model
				goOptMapAlias[ref.Model.Package] = ref.Model.PackageName
			}
			merges = service.Models
			merges.MergeMap(method.Models)
			if goOptMapAlias[sb.Scope.GetOptions().GetGoPackage()] != "" {
				reqName = goOptMapAlias[sb.Scope.GetOptions().GetGoPackage()] + "." + sb.Name
			}
			if goOptMapAlias[sb.Scope.GetOptions().GetGoPackage()] != "" {
				respName = goOptMapAlias[st.Scope.GetOptions().GetGoPackage()] + "." + st.Name
			}
			method.RequestTypeName = reqName
			method.ReturnTypeName = respName

			methods = append(methods, method)
		}

		service.Methods = methods
		service.Models = merges
		out = append(out, service)
	}
	return out, nil
}

//---------------------------------Model--------------------------------

func astToModel(ast *descriptorpb.FileDescriptorProto, rs *Resolver) (*model.Model, error) {
	main := rs.mainPkg.Model
	if main == nil {
		main = new(model.Model)
	}

	mainFileDes := rs.files.PbReflect[ast.GetName()]
	isProto3 := mainFileDes.IsProto3()
	// Enums
	ems := ast.GetEnumType()
	enums := make([]model.Enum, 0, len(ems))
	for _, e := range ems {
		em := model.Enum{
			Scope:  main,
			Name:   e.GetName(),
			GoType: "int32",
		}
		es := e.GetValue()
		vs := make([]model.Constant, 0, len(es))
		for _, ee := range es {
			vs = append(vs, model.Constant{
				Scope: main,
				Name:  ee.GetName(),
				Type:  model.TypeInt32,
				Value: model.IntExpression{Src: int(ee.GetNumber())},
			})
		}
		em.Values = vs
		enums = append(enums, em)
	}
	main.Enums = enums

	// Structs
	sts := ast.GetMessageType()
	structs := make([]model.Struct, 0, len(sts)*2)
	oneofs := make([]model.Oneof, 0, 1)
	for _, st := range sts {
		stMessage := mainFileDes.FindMessage(ast.GetPackage() + "." + st.GetName())
		stLeadingComments := getMessageLeadingComments(stMessage)
		s := model.Struct{
			Scope:           main,
			Name:            st.GetName(),
			Category:        model.CategoryStruct,
			LeadingComments: stLeadingComments,
		}

		ns := st.GetNestedType()
		nestedMessageInfoMap := getNestedMessageInfoMap(stMessage)
		for _, nt := range ns {
			if IsMapEntry(nt) {
				continue
			}

			nestedMessageInfo := nestedMessageInfoMap[nt.GetName()]
			nestedMessageLeadingComment := getMessageLeadingComments(nestedMessageInfo)
			s := model.Struct{
				Scope:           main,
				Name:            st.GetName() + "_" + nt.GetName(),
				Category:        model.CategoryStruct,
				LeadingComments: nestedMessageLeadingComment,
			}
			fs := nt.GetField()
			ns := nt.GetNestedType()
			vs := make([]model.Field, 0, len(fs))

			oneofMap := make(map[string]model.Field)
			oneofType, err := resolveOneof(nestedMessageInfo, oneofMap, rs, isProto3, s, ns)
			if err != nil {
				return nil, err
			}
			oneofs = append(oneofs, oneofType...)

			choiceSet := make(map[string]bool)

			for _, f := range fs {
				if field, exist := oneofMap[f.GetName()]; exist {
					if _, ex := choiceSet[field.Name]; !ex {
						choiceSet[field.Name] = true
						vs = append(vs, field)
					}
					continue
				}
				dv := f.GetDefaultValue()
				fieldLeadingComments, fieldTrailingComments := getFiledComments(f, nestedMessageInfo)
				t, err := rs.ResolveType(f, ns)
				if err != nil {
					return nil, err
				}
				field := model.Field{
					Scope:            &s,
					Name:             util.CamelString(f.GetName()),
					Type:             t,
					LeadingComments:  fieldLeadingComments,
					TrailingComments: fieldTrailingComments,
					IsPointer:        isPointer(f, isProto3),
				}
				if dv != "" {
					field.IsSetDefault = true
					field.DefaultValue, err = parseDefaultValue(f.GetType(), f.GetDefaultValue())
					if err != nil {
						return nil, err
					}
				}
				err = injectTagsToModel(f, &field, true)
				if err != nil {
					return nil, err
				}
				vs = append(vs, field)
			}
			checkDuplicatedFileName(vs)
			s.Fields = vs
			structs = append(structs, s)
		}

		fs := st.GetField()
		vs := make([]model.Field, 0, len(fs))

		oneofMap := make(map[string]model.Field)
		oneofType, err := resolveOneof(stMessage, oneofMap, rs, isProto3, s, ns)
		if err != nil {
			return nil, err
		}
		oneofs = append(oneofs, oneofType...)

		choiceSet := make(map[string]bool)

		for _, f := range fs {
			if field, exist := oneofMap[f.GetName()]; exist {
				if _, ex := choiceSet[field.Name]; !ex {
					choiceSet[field.Name] = true
					vs = append(vs, field)
				}
				continue
			}
			dv := f.GetDefaultValue()
			fieldLeadingComments, fieldTrailingComments := getFiledComments(f, stMessage)
			t, err := rs.ResolveType(f, ns)
			if err != nil {
				return nil, err
			}
			field := model.Field{
				Scope:            &s,
				Name:             util.CamelString(f.GetName()),
				Type:             t,
				LeadingComments:  fieldLeadingComments,
				TrailingComments: fieldTrailingComments,
				IsPointer:        isPointer(f, isProto3),
			}
			if dv != "" {
				field.IsSetDefault = true
				field.DefaultValue, err = parseDefaultValue(f.GetType(), f.GetDefaultValue())
				if err != nil {
					return nil, err
				}
			}
			err = injectTagsToModel(f, &field, true)
			if err != nil {
				return nil, err
			}
			vs = append(vs, field)
		}
		checkDuplicatedFileName(vs)
		s.Fields = vs
		structs = append(structs, s)

	}
	main.Oneofs = oneofs
	main.Structs = structs

	// In case of only the service refers another model, therefore scanning service is necessary
	ss := ast.GetService()
	for _, s := range ss {
		ms := s.GetMethod()
		for _, m := range ms {
			_, err := rs.ResolveIdentifier(m.GetInputType())
			if err != nil {
				return nil, err
			}
			_, err = rs.ResolveIdentifier(m.GetOutputType())
			if err != nil {
				return nil, err
			}
		}
	}

	return main, nil
}

// getMessageLeadingComments can get struct LeadingComment
func getMessageLeadingComments(stMessage *desc.MessageDescriptor) string {
	if stMessage == nil {
		return ""
	}
	stComments := stMessage.GetSourceInfo().GetLeadingComments()
	stComments = formatComments(stComments)

	return stComments
}

// getFiledComments can get field LeadingComments and field TailingComments for field
func getFiledComments(f *descriptorpb.FieldDescriptorProto, stMessage *desc.MessageDescriptor) (string, string) {
	if stMessage == nil {
		return "", ""
	}

	fieldNum := f.GetNumber()
	field := stMessage.FindFieldByNumber(fieldNum)
	fieldInfo := field.GetSourceInfo()

	fieldLeadingComments := fieldInfo.GetLeadingComments()
	fieldTailingComments := fieldInfo.GetTrailingComments()

	fieldLeadingComments = formatComments(fieldLeadingComments)
	fieldTailingComments = formatComments(fieldTailingComments)

	return fieldLeadingComments, fieldTailingComments
}

// formatComments can format the comments for beauty
func formatComments(comments string) string {
	if len(comments) == 0 {
		return ""
	}

	comments = util.TrimLastChar(comments)
	comments = util.AddSlashForComments(comments)

	return comments
}

// getNestedMessageInfoMap can get all nested struct
func getNestedMessageInfoMap(stMessage *desc.MessageDescriptor) map[string]*desc.MessageDescriptor {
	nestedMessage := stMessage.GetNestedMessageTypes()
	nestedMessageInfoMap := make(map[string]*desc.MessageDescriptor, len(nestedMessage))

	for _, nestedMsg := range nestedMessage {
		nestedMsgName := nestedMsg.GetName()
		nestedMessageInfoMap[nestedMsgName] = nestedMsg
	}

	return nestedMessageInfoMap
}

func parseDefaultValue(typ descriptorpb.FieldDescriptorProto_Type, val string) (model.Literal, error) {
	switch typ {
	case descriptorpb.FieldDescriptorProto_TYPE_BYTES, descriptorpb.FieldDescriptorProto_TYPE_STRING:
		return model.StringExpression{Src: val}, nil
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		return model.BoolExpression{Src: val == "true"}, nil
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
		descriptorpb.FieldDescriptorProto_TYPE_FLOAT,
		descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_UINT64,
		descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED64,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_ENUM,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED64,
		descriptorpb.FieldDescriptorProto_TYPE_SINT32,
		descriptorpb.FieldDescriptorProto_TYPE_SINT64:
		return model.NumberExpression{Src: val}, nil
	default:
		return nil, fmt.Errorf("unsupported type %s", typ.String())
	}
}

func isPointer(f *descriptorpb.FieldDescriptorProto, isProto3 bool) bool {
	if f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE || f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_BYTES {
		return false
	}

	if !isProto3 {
		if f.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
			return false
		}
		return true
	}

	switch f.GetLabel() {
	case descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL:
		if !f.GetProto3Optional() {
			return false
		}
		return true
	default:
		return false
	}
}

func resolveOneof(stMessage *desc.MessageDescriptor, oneofMap map[string]model.Field, rs *Resolver, isProto3 bool, s model.Struct, ns []*descriptorpb.DescriptorProto) ([]model.Oneof, error) {
	oneofs := make([]model.Oneof, 0, 1)
	if len(stMessage.GetOneOfs()) != 0 {
		for _, oneof := range stMessage.GetOneOfs() {
			if isProto3 {
				if oneof.IsSynthetic() {
					continue
				}
			}
			oneofName := oneof.GetName()
			messageName := s.Name
			typeName := "is" + messageName + "_" + oneofName
			field := model.Field{
				Scope:     &s,
				Name:      util.CamelString(oneofName),
				Type:      model.NewOneofType(typeName),
				IsPointer: false,
			}

			oneofComment := oneof.GetSourceInfo().GetLeadingComments()
			oneofComment = formatComments(oneofComment)
			var oneofLeadingComments string
			if oneofComment == "" {
				oneofLeadingComments = fmt.Sprintf(" Types that are assignable to %s:\n", oneofName)
			} else {
				oneofLeadingComments = fmt.Sprintf("%s\n//\n// Types that are assignable to %s:\n", oneofComment, oneofName)
			}
			for idx, ch := range oneof.GetChoices() {
				if idx == len(oneof.GetChoices())-1 {
					oneofLeadingComments = oneofLeadingComments + fmt.Sprintf("//  *%s_%s", messageName, ch.GetName())
				} else {
					oneofLeadingComments = oneofLeadingComments + fmt.Sprintf("//  *%s_%s\n", messageName, ch.GetName())
				}
			}
			field.LeadingComments = oneofLeadingComments

			choices := make([]model.Choice, 0, len(oneof.GetChoices()))
			for _, ch := range oneof.GetChoices() {
				t, err := rs.ResolveType(ch.AsFieldDescriptorProto(), ns)
				if err != nil {
					return nil, err
				}
				choice := model.Choice{
					MessageName: messageName,
					ChoiceName:  ch.GetName(),
					Type:        t,
				}
				choices = append(choices, choice)
				oneofMap[ch.GetName()] = field
			}

			oneofType := model.Oneof{
				MessageName:   messageName,
				OneofName:     oneofName,
				InterfaceName: typeName,
				Choices:       choices,
			}

			oneofs = append(oneofs, oneofType)
		}
	}
	return oneofs, nil
}

func getNewFieldName(fieldName string, fieldNameSet map[string]bool) string {
	if _, ex := fieldNameSet[fieldName]; ex {
		fieldName = fieldName + "_"
		return getNewFieldName(fieldName, fieldNameSet)
	}
	return fieldName
}

func checkDuplicatedFileName(vs []model.Field) {
	fieldNameSet := make(map[string]bool)
	for i := 0; i < len(vs); i++ {
		if _, ex := fieldNameSet[vs[i].Name]; ex {
			newName := getNewFieldName(vs[i].Name, fieldNameSet)
			fieldNameSet[newName] = true
			vs[i].Name = newName
		} else {
			fieldNameSet[vs[i].Name] = true
		}
	}
}
