package eventhandlers

import (
	"context"
	"fmt"
	"github.com/golang/glog"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/aws/aws-application-networking-k8s/pkg/config"
)

type enqueueRequestsForGatewayEvent struct {
	client client.Client
}

func NewEnqueueRequestGatewayEvent(client client.Client) handler.EventHandler {
	return &enqueueRequestsForGatewayEvent{
		client: client,
	}
}

var ZeroTransitionTime = metav1.NewTime(time.Time{})

func (h *enqueueRequestsForGatewayEvent) Create(e event.CreateEvent, queue workqueue.RateLimitingInterface) {
	glog.V(6).Info("Gateway Create")
	gwNew := e.Object.(*v1alpha2.Gateway)

	// initialize transition time
	gwNew.Status.Conditions[0].LastTransitionTime = ZeroTransitionTime
	h.enqueueImpactedHTTPRoute(queue, gwNew)
}

func (h *enqueueRequestsForGatewayEvent) Update(e event.UpdateEvent, queue workqueue.RateLimitingInterface) {
	glog.V(6).Info("Gateway Update ")

	gwOld := e.ObjectOld.(*v1alpha2.Gateway)
	gwNew := e.ObjectNew.(*v1alpha2.Gateway)

	if !equality.Semantic.DeepEqual(gwOld.Spec, gwNew.Spec) {
		// initialize transition time
		gwNew.Status.Conditions[0].LastTransitionTime = ZeroTransitionTime
		h.enqueueImpactedHTTPRoute(queue, gwNew)
	}
}

func (h *enqueueRequestsForGatewayEvent) Delete(e event.DeleteEvent, queue workqueue.RateLimitingInterface) {
	fmt.Printf("TODO Gatway Delete")
}

func (h *enqueueRequestsForGatewayEvent) Generic(e event.GenericEvent, queue workqueue.RateLimitingInterface) {

}

func (h *enqueueRequestsForGatewayEvent) enqueueImpactedHTTPRoute(queue workqueue.RateLimitingInterface, gw *v1alpha2.Gateway) {
	httpRouteList := &v1alpha2.HTTPRouteList{}

	h.client.List(context.TODO(), httpRouteList)

	for _, httpRoute := range httpRouteList.Items {

		if len(httpRoute.Spec.ParentRefs) <= 0 {
			glog.V(6).Infof("Ingnore httpRoute no parentRefs %s", httpRoute.Name)
			continue
		}

		// find the parent gw object
		// TODO gw is in default namespace
		gwName := types.NamespacedName{
			Namespace: "default",
			Name:      string(httpRoute.Spec.ParentRefs[0].Name),
		}

		gw := &v1alpha2.Gateway{}

		if err := h.client.Get(context.TODO(), gwName, gw); err != nil {
			glog.V(6).Infof("Ignore HTTPRoute with unknow parentRef %s\n", httpRoute.Name)
			continue
		}

		// find the parent gateway class name
		gwClass := &v1alpha2.GatewayClass{}
		gwClassName := types.NamespacedName{
			Namespace: "default",
			Name:      string(gw.Spec.GatewayClassName),
		}

		if err := h.client.Get(context.TODO(), gwClassName, gwClass); err != nil {
			glog.V(6).Infof("Ignore HTTPRoute with unknown Gateway %s \n", httpRoute.Name)
			continue
		}

		if gwClass.Spec.ControllerName == config.LatticeGatewayControllerName {
			glog.V(6).Infof("Trigger HTTPRoute from Gateway event , httpRoute %s", httpRoute.Name)
			queue.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: httpRoute.Namespace,
					Name:      httpRoute.Name,
				},
			})
		}

	}
}