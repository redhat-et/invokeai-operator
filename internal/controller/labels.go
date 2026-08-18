/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	invokeaiv1alpha1 "github.com/red-hat-et/invokeai-operator/api/v1alpha1"
)

func labelsForInvokeAI(platform *invokeaiv1alpha1.InvokeAIPlatform) map[string]string {
	return map[string]string{
		labelName:      appName,
		labelInstance:  platform.Name,
		labelComponent: appName,
		labelManagedBy: operatorName,
	}
}

func selectorLabelsForInvokeAI(platform *invokeaiv1alpha1.InvokeAIPlatform) map[string]string {
	return map[string]string{
		labelName:      appName,
		labelInstance:  platform.Name,
		labelComponent: appName,
	}
}

func labelsForBackend(platform *invokeaiv1alpha1.InvokeAIPlatform, backendName string) map[string]string {
	return map[string]string{
		labelName:      appName,
		labelInstance:  platform.Name,
		labelComponent: backendName,
		labelManagedBy: operatorName,
	}
}
