package mongodb

import "github.com/riducms/ridu/internal/environment"

// ConfigFromEnvironment reads a Ridu application's standard MongoDB
// variables: DATABASE_URL, and RIDU_ALLOW_INSECURE_DATABASE for a remote
// development database whose transport is secured outside MongoDB. Set other
// Config fields, such as ApplicationName, on the result before opening the
// store.
func ConfigFromEnvironment() (Config, error) {
	insecure, err := environment.Bool("RIDU_ALLOW_INSECURE_DATABASE")
	if err != nil {
		return Config{}, err
	}
	return Config{DatabaseURL: environment.Text("DATABASE_URL"), AllowInsecureTransport: insecure}, nil
}
