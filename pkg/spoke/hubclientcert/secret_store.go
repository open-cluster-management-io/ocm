package hubclientcert

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

// SecretReader provides functions to read data from a designated secret
type SecretReader interface {
	Get(key string) ([]byte, bool, error)
	GetString(key string) (string, bool, error)
	GetData() (map[string][]byte, error)
}

// SecretWritter provides functions to write data into the designated secret
type SecretWritter interface {
	Set(key string, value []byte) (bool, error)
	SetString(key string, value string) (bool, error)
	SetData(data map[string][]byte) (bool, error)
	Remove(keys ...string) (bool, error)
}

type SecretStore interface {
	SecretReader
	SecretWritter
}

type secretStore struct {
	secretNamespace string
	secretName      string

	coreClient corev1client.CoreV1Interface
}

// NewSecretReader return a concrete implementation of SecretReader.
func NewSecretReader(secretKey string, coreClient corev1client.CoreV1Interface) (SecretReader, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(secretKey)
	if err != nil {
		return nil, fmt.Errorf("invalid secret name %q: %v", secretKey, err)
	}

	return &secretStore{
		secretNamespace: namespace,
		secretName:      name,
		coreClient:      coreClient,
	}, nil
}

// NewSecretStore return a concrete implementation of SecretStore.
func NewSecretStore(secretKey string, coreClient corev1client.CoreV1Interface) (SecretStore, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(secretKey)
	if err != nil {
		return nil, fmt.Errorf("invalid secret name %q: %v", secretKey, err)
	}

	return &secretStore{
		secretNamespace: namespace,
		secretName:      name,
		coreClient:      coreClient,
	}, nil
}

func (s *secretStore) getSecret() (*corev1.Secret, bool, error) {
	secret, err := s.coreClient.Secrets(s.secretNamespace).Get(context.Background(), s.secretName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("unable to get secret %q: %v", s.secretNamespace+"/"+s.secretName, err)
	}

	return secret, true, nil
}

func (s *secretStore) Get(key string) ([]byte, bool, error) {
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

func (s *secretStore) Set(key string, value []byte) (bool, error) {
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

func (s *secretStore) SetString(key string, value string) (bool, error) {
	return s.Set(key, []byte(value))
}

func (s *secretStore) SetData(data map[string][]byte) (bool, error) {
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

func (s *secretStore) GetString(key string) (string, bool, error) {
	value, exists, err := s.Get(key)
	if err != nil || !exists {
		return "", exists, err
	}
	return string(value), true, nil
}

func (s *secretStore) GetData() (map[string][]byte, error) {
	secret, exist, err := s.getSecret()
	if err != nil {
		return nil, err
	}

	if exist && secret.Data != nil {
		return secret.Data, nil
	}

	return map[string][]byte{}, nil
}

func (s *secretStore) Remove(keys ...string) (bool, error) {
	secret, exists, err := s.getSecret()
	if err != nil {
		return false, err
	}

	if !exists {
		return false, nil
	}

	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}

	changed := false
	for _, key := range keys {
		if _, ok := secret.Data[key]; ok {
			delete(secret.Data, key)
			changed = true
		}
	}

	if !changed {
		return false, nil
	}

	_, err = s.coreClient.Secrets(s.secretNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
	if err != nil {
		return true, fmt.Errorf("unable to remove %q from secret %q: %v", keys, s.secretNamespace+"/"+s.secretName, err)
	}

	klog.V(4).Infof("Data %q removed from secret %q", keys, s.secretNamespace+"/"+s.secretName)
	return true, nil
}
