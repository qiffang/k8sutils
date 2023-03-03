package exec

import (
	"context"
	"fmt"
	"github.com/pingcap/log"
	"go.uber.org/zap"
	"io"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	spdy2 "k8s.io/client-go/transport/spdy"
	"net/http"
	"net/url"
	"strings"
	"time"

	authzv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

var deniedCreateExecErr = fmt.Errorf("no permissions to create exec subresource")

// ExecPod issues an exec request to execute the given command to a particular
// pod.
func (c *Client) ExecPod(command []string, stdin io.Reader, stdout, stderr io.Writer, tty bool, timeout time.Duration) error {
	log.Info("sending exec request, command=%s, namespace=%S, pod=%s, container=%s", zap.String("command", strings.Join(command, " ")), zap.String("namespace", c.Namespace), zap.String("pod", c.PodName), zap.String("container", c.ContainerName), zap.String("timeout", timeout.String()))

	execRequest := c.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(c.Namespace).
		Name(c.PodName).
		SubResource("exec").
		Timeout(timeout)

	execRequest = execRequest.VersionedParams(&corev1.PodExecOptions{
		Container: c.ContainerName,
		Command:   command,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    stderr != nil,
		TTY:       tty,
	}, scheme.ParameterCodec)

	exec, err := newExecutor(c.K8sConfig, "POST", execRequest.URL())
	if err != nil {
		return fmt.Errorf("failed to set up executor: %w", err)
	}

	if err := exec.Stream(remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    tty,
	}); err != nil {
		return fmt.Errorf("failed to exec command: %w", err)
	}

	return nil
}

// CanExec determines if the current user can create a exec subresource in the
// given pod.
func (c *Client) CanExec() error {
	selfAccessReview := &authzv1.SelfSubjectAccessReview{
		Spec: authzv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authzv1.ResourceAttributes{
				Namespace:   c.Namespace,
				Verb:        "create",
				Group:       "",
				Resource:    "pods",
				Subresource: "exec",
				Name:        "",
			},
		},
	}

	log.Info("checking for exec permissions.", zap.String("namespace", c.Namespace))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	response, err := c.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, selfAccessReview, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	if !response.Status.Allowed {
		if response.Status.Reason != "" {
			return fmt.Errorf("%w. reason: %s", deniedCreateExecErr, response.Status.Reason)
		}
		return deniedCreateExecErr
	}

	log.Info("confirmed exec permissions.", zap.String("namespace", c.Namespace))
	return nil
}

var newExecutor = func(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error) {
	return NewSPDYExecutor(config, method, url)
}

func NewSPDYExecutor(config *restclient.Config, method string, url *url.URL) (remotecommand.Executor, error) {
	wrapper, upgradeRoundTripper, err := RoundTripperFor(config)
	if err != nil {
		return nil, err
	}
	return remotecommand.NewSPDYExecutorForTransports(wrapper, upgradeRoundTripper, method, url)
}

func RoundTripperFor(config *restclient.Config) (http.RoundTripper, spdy2.Upgrader, error) {
	tlsConfig, err := restclient.TLSConfigFor(config)
	if err != nil {
		return nil, nil, err
	}
	proxy := http.ProxyFromEnvironment
	if config.Proxy != nil {
		proxy = config.Proxy
	}
	upgradeRoundTripper := spdy.NewRoundTripperWithConfig(spdy.RoundTripperConfig{
		TLS:                      tlsConfig,
		FollowRedirects:          true,
		RequireSameHostRedirects: false,
		Proxier:                  proxy,
		PingPeriod:               0,
	})
	wrapper, err := restclient.HTTPWrappersForConfig(config, upgradeRoundTripper)
	if err != nil {
		return nil, nil, err
	}
	return wrapper, upgradeRoundTripper, nil
}
