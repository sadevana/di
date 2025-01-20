package di

import (
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
)

// TODO:
// 1) allow dst struct to have nested structs
// For example, repos and services can be nested in the App struct
//
//		type Repos struct {
//			UserRepo *UserRepo
//			PostRepo *PostRepo
//		}
//
//	 type Services struct {
//			UserService *UserService
//			PostService *PostService
//		}
//	 type App struct {
//			Repos *Repos
//			Services *Services
//		}
//
// 2) ability to provide dependency sub-structs
// For example, userRepo and postRepo are provided in the Repos struct,
// but userService and postService are dependent on them, not Repos
/*
	type Repos struct {
		UserRepo *UserRepo
		PostRepo *PostRepo
	}

	func reposProvider() *Repos {
		return &Repos{
			UserRepo: userRepoProvider(),
			PostRepo: postRepoProvider(),
		}
	}

	func userServiceProvider(userRepo *UserRepo) *UserService {
		return &UserService{
			UserRepo: userRepo,
		}
	}

	func postServiceProvider(postRepo *PostRepo) *PostService {
		return &PostService{
			PostRepo: postRepo,
		}
	}

	provider, err := di.NewProvider(reposProvider, userServiceProvider, postServiceProvider)
	if err != nil {
		panic(err)
	}

	app := &App{}
	err = provider.Provide(app)
	if err != nil {
		panic(err)
	}
*/
// Provider can fill the destination with the dependencies.
// Example usage:
//
//		provider, err := di.NewProvider(
//			func() (*MyDependency, error) {
//				return &MyDependency{}, nil
//			},
//			func() (*MyOtherDependency, error) {
//				return &MyOtherDependency{}, nil
//			},
//	     func(*MyDependency, *MyOtherDependency) (*MyService, error) {
//				return &MyService{}, nil
//			},
//		)
//
//	 type Deps struct {
//			MyDependency *MyDependency
//			MyOtherDependency *MyOtherDependency
//			MyService *MyService
//		}
//		provider.Provide(&Deps{})
type Provider struct {
	allProvidedTypes map[reflect.Type]providerInfo
	resolvedTypes    map[reflect.Type]reflect.Value
}

func NewProvider(depProviders ...any) (*Provider, error) {
	allProvidedTypes, err := parseProviders(depProviders...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse providers: %w", err)
	}

	if err := allSubDepsProvided(allProvidedTypes); err != nil {
		return nil, fmt.Errorf("all deps must be provided: %w", err)
	}

	if err := checkForCyclicDependencies(allProvidedTypes); err != nil {
		return nil, fmt.Errorf("should not have cyclic dependencies: %w", err)
	}

	return &Provider{
		allProvidedTypes: allProvidedTypes,
		resolvedTypes:    make(map[reflect.Type]reflect.Value),
	}, nil
}

type providerInfo struct {
	providerName string
	providedType reflect.Type
	deps         []reflect.Type
	provider     reflect.Value // function
}

func parseProviders(depProviders ...any) (map[reflect.Type]providerInfo, error) {
	parsed := make(map[reflect.Type]providerInfo, len(depProviders))
	for i, provider := range depProviders {
		providerType := reflect.TypeOf(provider)
		if providerType.Kind() != reflect.Func {
			return nil, fmt.Errorf("%dth provider is not a function, got %s", i, providerType.Kind())
		}
		providerName, err := getFunctionName(provider)
		if err != nil {
			return nil, fmt.Errorf("failed to get function name for provider %d: %w", i, err)
		}
		outCount := providerType.NumOut()
		if outCount < 1 {
			return nil, fmt.Errorf("%dth provider %s has no output", i, providerName)
		}
		if outCount > 2 {
			return nil, fmt.Errorf("%dth provider %s has more than two outputs. Provider must return a single value or a value and an error", i, providerName)
		}
		if outCount == 2 {
			if providerType.Out(1).Kind() != reflect.Interface || providerType.Out(1).String() != "error" {
				return nil, fmt.Errorf("%dth provider %s has two outputs, but the second one is not an error", i, providerName)
			}
		}
		out := providerType.Out(0)
		if duplicateProvider, ok := parsed[out]; ok {
			return nil, fmt.Errorf("%dth provider %s returns the same type %s as provider %s", i, providerName, out, duplicateProvider.providerName)
		}

		deps := make([]reflect.Type, 0, providerType.NumIn())
		for j := 0; j < providerType.NumIn(); j++ {
			deps = append(deps, providerType.In(j))
		}

		parsed[out] = providerInfo{
			providerName: providerName,
			providedType: out,
			deps:         deps,
			provider:     reflect.ValueOf(provider),
		}
	}

	return parsed, nil
}

func getFunctionName(fn any) (string, error) {
	funcRuntime := runtime.FuncForPC(reflect.ValueOf(fn).Pointer())
	if funcRuntime == nil {
		return "", fmt.Errorf("failed to get function runtime for %v", fn)
	}
	fullName := funcRuntime.Name()
	// Extract the last part after the last '.' to get the function name
	lastDot := strings.LastIndex(fullName, ".")
	return fullName[lastDot+1:], nil
}

func allSubDepsProvided(allProvidedTypes map[reflect.Type]providerInfo) error {
	for _, provider := range allProvidedTypes {
		for _, dep := range provider.deps {
			if _, ok := allProvidedTypes[dep]; !ok {
				return fmt.Errorf("dependency %s is not provided", dep)
			}
		}
	}
	return nil
}

func checkForCyclicDependencies(allProvidedTypes map[reflect.Type]providerInfo) error {
	for providerType, provider := range allProvidedTypes {
		providerDeps := provider.deps
		for otherProviderType, otherProvider := range allProvidedTypes {
			if providerType == otherProviderType {
				continue
			}
			otherDepsContainsProvider := slices.Contains(otherProvider.deps, providerType)
			providerDepsContainsOther := slices.Contains(providerDeps, otherProviderType)
			if otherDepsContainsProvider && providerDepsContainsOther {
				return fmt.Errorf("cyclic dependency found between providers %s and %s", provider.providerName, otherProvider.providerName)
			}
		}
	}
	return nil
}

func (c *Provider) Provide(dst any) error {
	dstType := reflect.TypeOf(dst)
	if dstType.Kind() != reflect.Ptr || dstType.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("destination must be a pointer to a struct, got %s", dstType.Kind())
	}
	dstValue := reflect.ValueOf(dst).Elem()
	for i := 0; i < dstValue.NumField(); i++ {
		field := dstValue.Field(i)
		fieldType := dstType.Elem().Field(i)
		diTag := fieldType.Tag.Get("di")
		switch diTag {
		case "group":
			if err := c.resolveGroup(field, fieldType); err != nil {
				return fmt.Errorf("failed to resolve group %s: %w", fieldType.Name, err)
			}
		case "-":
			continue
		default:
			fieldValue, err := c.resolve(fieldType.Type)
			if err != nil {
				return fmt.Errorf("failed to resolve field %s: %w", fieldType.Name, err)
			}
			field.Set(fieldValue)
		}
	}

	return nil
}

func (c *Provider) resolve(fieldType reflect.Type) (reflect.Value, error) {
	if value, ok := c.resolvedTypes[fieldType]; ok {
		return value, nil
	}

	provider, ok := c.allProvidedTypes[fieldType]
	if !ok {
		return reflect.Value{}, fmt.Errorf("no provider found for type %s", fieldType)
	}

	resolvedDeps := make([]reflect.Value, 0, len(provider.deps))
	for _, depType := range provider.deps {
		depValue, err := c.resolve(depType)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("failed to resolve dependency %s: %w", depType, err)
		}
		resolvedDeps = append(resolvedDeps, depValue)
	}

	results := provider.provider.Call(resolvedDeps)
	if len(results) == 2 && results[1].Interface() != nil {
		resolutionError := results[1].Interface().(error)
		return reflect.Value{}, fmt.Errorf("%s failed to resolve value: %w", provider.providerName, resolutionError)
	}
	resolvedValue := results[0]

	c.resolvedTypes[fieldType] = resolvedValue

	return resolvedValue, nil
}

func (c *Provider) resolveGroup(field reflect.Value, fieldType reflect.StructField) error {
	if field.Kind() != reflect.Struct {
		return fmt.Errorf("`di:group` can only be used on struct fields, got %s", field.Kind())
	}
	if !field.CanInterface() {
		return fmt.Errorf("`di:group` can only be used on exported struct fields, got %s", fieldType.Name)
	}
	if !field.CanAddr() {
		return fmt.Errorf("`di:group` can only be used on addressable struct fields, got %s", fieldType.Name)
	}
	if err := c.Provide(field.Addr().Interface()); err != nil {
		return fmt.Errorf("failed to provide group %s: %w", fieldType.Name, err)
	}
	return nil
}
