# Introduce 

Introduce 2 libs `workBuilder` and `workApplier` to help user to build the manifest objects into manifestWorks 
and apply/delete the manifestWorks easily.

# Usage

## `workBuilder`

There is a limit size of manifest objects in a single manifestWork. And currently the limit is 50K.
if the size of the manifest objects exceed the limit, `workBuilder` can help build the manifest objects into 
multiple manifestWorks and update the existing manifestWorks.

1. Create a `WorkBuilder` instance.

    ```go
    import (
        "open-cluster-management.io/addon-framework/pkg/common/workbuilder"
    )
        workBuilder := workbuilder.NewWorkBuilder()
    ```
    
    `WorkBuilder` will build a manifestWork with 80% size of the limit, the user can customize the limit size for the manifestWorks.
    
    ```go
        workBuilder := workbuilder.NewWorkBuilder().WithManifestsLimit(limtSize)
    ```

2. Define a `GenerateManifestWorkObjectMeta` func to generate the meta info of the new manifestWorks like name, namespace, labels etc.
The input `index` is the count of the existing manifestWorks. 

    ```go
     func myGenerateWorkObjectMeta(index int) metav1.ObjectMeta {
         return metav1.ObjectMeta{
             Name:      <unique name for the manifestWork>,
             Namespace: clusterNamespace,
         }
     }
    ```

3. Build the manifest objects into manifestWorks if there is no existing manifestWorks.

    ```go
    applyWorks, _, err := workBuilder.Build(manifestObjects, myGenerateWorkObjectMeta)
    ```
    The input `manifestObjects` is a slice of `runtime.Object`. 
    And the output`applyWorks` is a slice of built manifestWorks.
   
4. Build the manifest objects into manifestWorks to update the existing manifestWorks on the Hub cluster.

    ```go
    applyWorks, deleteWorks, err := workBuilder.Build(manifestObjects, myGenerateWorkObjectMeta,
	    workbuilder.ExistingManifestWorksOption(existingWorks))
    ```
    The `existingWorks` is a slice of existing manifestWorks including the old manifests objects.
   
    The `applyWorks` is a slice of manifestWorks including the new created manifestWorks and 
    the updated existing manifestWorks.
   
    The `deleteWorks` is a slice of manifestWorks which needs to be deleted since all its manifests are removed.
      
5. Build manifestWorks with Options. 
    
    The user can configure the `ManifestConfigs`,`Executor` or `DeleteOption` as `WorkBuilderOption` to the built manifestWorks.
    ```go
    applyWorks, deleteWorks, err := workBuilder.Build(manifestObjects, myGenerateWorkObjectMeta,
	    workbuilder.ExistingManifestWorksOption(existingWorks),
        workbuilder.ManifestConfigOption(manifestOptions),
        workbuilder.ManifestWorkExecutorOption(executorOption),
        workbuilder.DeletionOption(deletionOption))
    ```

6. Build and apply the manifestWorks.

    The user also can use `BuildAndApply` method to apply the built manifestWorks with a `workApplier` instance.

    ```go
    err := workBuilder.BuildAndApply(context, manifestObjects, myGenerateWorkObjectMeta,
        workApplier,
	    workbuilder.ExistingManifestWorksOption(existingWorks),
        workbuilder.ManifestConfigOption(manifestOptions),
        workbuilder.ManifestWorkExecutorOption(executorOption),
        workbuilder.DeletionOption(deletionOption))
    ```
   
## `workApplier`

`workApplier` can help the user to apply or delete a manifestWork easily, which has a cache to reduce redundant updates.

1. Create a `WorkApplier` instance.

    There are 2 types to create a `WorkApplier` instance.
    
    One is `NewWorkApplierWithTypedClient(workClient workv1client.Interface, workLister worklister.ManifestWorkLister)` 
    with manifestWork typed client. 

    Another is`NewWorkApplierWithRuntimeClient(workClient client.Client)` with runtime client.

2. Apply a manifestWork.
    
    This method will create the manifestWork if the manifestWork does not exist, and will update the existing manifestWork.
   ```
   appliedWork, err := workApplier.Apply(context, manifestWork)
   ```

3. Delete a manifestWork.
   ```
   err := workApplier.Delete(context, namesapce, name)
   ```
