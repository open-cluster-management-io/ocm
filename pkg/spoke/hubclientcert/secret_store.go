package hubclientcert

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog"
)

// SecretStore provides functions to read/write data from/into a designated secret
type SecretStore struct {
	secretNamespace string
	secretName      string

	coreClient corev1client.CoreV1Interface
}

// NewSecretStore return a SecretStore.
func NewSecretStore(secretNamespace, secretName string, coreClient corev1client.CoreV1Interface) *SecretStore {
	return &SecretStore{
		secretNamespace: secretNamespace,
		secretName:      secretName,
		coreClient:      coreClient,
	}
}

func (s *SecretStore) getSecret() (*corev1.Secret, bool, error) {
	secret, err := s.coreClient.Secrets(s.secretNamespace).Get(context.Background(), s.secretName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("unable to get secret %q: %v", s.secretNamespace+"/"+s.secretName, err)
	}

	return secret, true, nil
}

// Get returns data entry in the secret data with a given key
func (s *SecretStore) Get(key string) ([]byte, bool, error) {
	secret, exists, err := s.getSecret()
	if err != nil || !exists {
		return nil, exists, err
	}

	if secret.Data == nil {
		return nil, false, nil
	}

	if value, ok := secret.Data[key]; ok {
		return value, true, nil
	}

	return nil, false, nil
}

// GetString returns data entry as string in the secret data with a given key
func (s *SecretStore) GetString(key string) (string, bool, error) {
	value, exists, err := s.Get(key)
	if err != nil || !exists {
		return "", exists, err
	}
	return string(value), true, nil
}

// GetData returns the whole data map in the secret
func (s *SecretStore) GetData() (map[string][]byte, error) {
	secret, exist, err := s.getSecret()
	if err != nil {
		return nil, err
	}

	if exist && secret.Data != nil {
		return secret.Data, nil
	}

	return map[string][]byte{}, nil
}

// Set sets the value of a particular data entry in the secret data
func (s *SecretStore) Set(key string, value []byte) (bool, error) {
	secret, exists, err := s.getSecret()
	if err != nil {
		return false, err
	}

	if exists {
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		if v, ok := secret.Data[key]; ok {
			if reflect.DeepEqual(value, v) {
				return false, nil
			}
		}
		secret.Data[key] = value
		_, err = s.coreClient.Secrets(s.secretNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
	} else {
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: s.secretNamespace,
				Name:      s.secretName,
			},
			Data: map[string][]byte{
				key: value,
			},
		}
		_, err = s.coreClient.Secrets(s.secretNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
	}

	if err != nil {
		return true, fmt.Errorf("unable to save %q in secret %q: %v", key, s.secretNamespace+"/"+s.secretName, err)
	}

	klog.V(4).Infof("%q saved in secret %q", key, s.secretNamespace+"/"+s.secretName)
	return true, nil
}

// SetString sets the value of a particular data entry in the secret data
func (s *SecretStore) SetString(key string, value string) (bool, error) {
	return s.Set(key, []byte(value))
}

// SetData sets the secret data
func (s *SecretStore) SetData(data map[string][]byte) (bool, error) {
	secret, exists, err := s.getSecret()
	if err != nil {
		return false, err
	}

	if exists {
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		if reflect.DeepEqual(data, secret.Data) {
			return false, nil
		}
		secret.Data = data
		_, err = s.coreClient.Secrets(s.secretNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
	} else {
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: s.secretNamespace,
				Name:      s.secretName,
			},
			Data: data,
		}
		_, err = s.coreClient.Secrets(s.secretNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
	}

	if err != nil {
		return true, fmt.Errorf("unable to save data in secret %q: %v", s.secretNamespace+"/"+s.secretName, err)
	}

	klog.V(4).Infof("Data saved in secret %q", s.secretNamespace+"/"+s.secretName)
	return true, nil
}
