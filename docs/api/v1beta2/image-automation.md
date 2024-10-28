<h1>Image update automation API reference v1beta2</h1>
<p>Packages:</p>
<ul class="simple">
<li>
<a href="#image.toolkit.fluxcd.io%2fv1beta2">image.toolkit.fluxcd.io/v1beta2</a>
</li>
</ul>
<h2 id="image.toolkit.fluxcd.io/v1beta2">image.toolkit.fluxcd.io/v1beta2</h2>
<p>Package v1beta2 contains API types for the image API group, version
v1beta2. The types here are concerned with automated updates to
git, based on metadata from OCI image registries gathered by the
image-reflector-controller.</p>
Resource Types:
<ul class="simple"></ul>
<h3 id="image.toolkit.fluxcd.io/v1beta2.CommitSpec">CommitSpec
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.GitSpec">GitSpec</a>)
</p>
<p>CommitSpec specifies how to commit changes to the git repository</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>author</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.CommitUser">
CommitUser
</a>
</em>
</td>
<td>
<p>Author gives the email and optionally the name to use as the
author of commits.</p>
</td>
</tr>
<tr>
<td>
<code>signingKey</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.SigningKey">
SigningKey
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>SigningKey provides the option to sign commits with a GPG key</p>
</td>
</tr>
<tr>
<td>
<code>messageTemplate</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>MessageTemplate provides a template for the commit message,
into which will be interpolated the details of the change made.</p>
</td>
</tr>
<tr>
<td>
<code>messageTemplateValues</code><br>
<em>
map[string]string
</em>
</td>
<td>
<p>MessageTemplateValues provides additional values to be available to the
templating rendering.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.CommitUser">CommitUser
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.CommitSpec">CommitSpec</a>)
</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Name gives the name to provide when making a commit.</p>
</td>
</tr>
<tr>
<td>
<code>email</code><br>
<em>
string
</em>
</td>
<td>
<p>Email gives the email to provide when making a commit.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.CrossNamespaceSourceReference">CrossNamespaceSourceReference
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.ImageUpdateAutomationSpec">ImageUpdateAutomationSpec</a>)
</p>
<p>CrossNamespaceSourceReference contains enough information to let you locate the
typed Kubernetes resource object at cluster level.</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>apiVersion</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>API version of the referent.</p>
</td>
</tr>
<tr>
<td>
<code>kind</code><br>
<em>
string
</em>
</td>
<td>
<p>Kind of the referent.</p>
</td>
</tr>
<tr>
<td>
<code>name</code><br>
<em>
string
</em>
</td>
<td>
<p>Name of the referent.</p>
</td>
</tr>
<tr>
<td>
<code>namespace</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Namespace of the referent, defaults to the namespace of the Kubernetes resource object that contains the reference.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.GitCheckoutSpec">GitCheckoutSpec
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.GitSpec">GitSpec</a>)
</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>ref</code><br>
<em>
<a href="https://pkg.go.dev/github.com/fluxcd/source-controller/api/v1#GitRepositoryRef">
Source /v1.GitRepositoryRef
</a>
</em>
</td>
<td>
<p>Reference gives a branch, tag or commit to clone from the Git
repository.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.GitSpec">GitSpec
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.ImageUpdateAutomationSpec">ImageUpdateAutomationSpec</a>)
</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>checkout</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.GitCheckoutSpec">
GitCheckoutSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Checkout gives the parameters for cloning the git repository,
ready to make changes. If not present, the <code>spec.ref</code> field from the
referenced <code>GitRepository</code> or its default will be used.</p>
</td>
</tr>
<tr>
<td>
<code>commit</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.CommitSpec">
CommitSpec
</a>
</em>
</td>
<td>
<p>Commit specifies how to commit to the git repository.</p>
</td>
</tr>
<tr>
<td>
<code>push</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.PushSpec">
PushSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Push specifies how and where to push commits made by the
automation. If missing, commits are pushed (back) to
<code>.spec.checkout.branch</code> or its default.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.ImageRef">ImageRef
</h3>
<p>ImageRef represents an image reference.</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br>
<em>
string
</em>
</td>
<td>
<p>Name is the bare image&rsquo;s name.</p>
</td>
</tr>
<tr>
<td>
<code>tag</code><br>
<em>
string
</em>
</td>
<td>
<p>Tag is the image&rsquo;s tag.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.ImageUpdateAutomation">ImageUpdateAutomation
</h3>
<p>ImageUpdateAutomation is the Schema for the imageupdateautomations API</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>metadata</code><br>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta
</a>
</em>
</td>
<td>
Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>
</tr>
<tr>
<td>
<code>spec</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.ImageUpdateAutomationSpec">
ImageUpdateAutomationSpec
</a>
</em>
</td>
<td>
<br/>
<br/>
<table>
<tr>
<td>
<code>sourceRef</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.CrossNamespaceSourceReference">
CrossNamespaceSourceReference
</a>
</em>
</td>
<td>
<p>SourceRef refers to the resource giving access details
to a git repository.</p>
</td>
</tr>
<tr>
<td>
<code>git</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.GitSpec">
GitSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>GitSpec contains all the git-specific definitions. This is
technically optional, but in practice mandatory until there are
other kinds of source allowed.</p>
</td>
</tr>
<tr>
<td>
<code>interval</code><br>
<em>
<a href="https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
Kubernetes meta/v1.Duration
</a>
</em>
</td>
<td>
<p>Interval gives an lower bound for how often the automation
run should be attempted.</p>
</td>
</tr>
<tr>
<td>
<code>policySelector</code><br>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#labelselector-v1-meta">
Kubernetes meta/v1.LabelSelector
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>PolicySelector allows to filter applied policies based on labels.
By default includes all policies in namespace.</p>
</td>
</tr>
<tr>
<td>
<code>update</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.UpdateStrategy">
UpdateStrategy
</a>
</em>
</td>
<td>
<p>Update gives the specification for how to update the files in
the repository. This can be left empty, to use the default
value.</p>
</td>
</tr>
<tr>
<td>
<code>suspend</code><br>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Suspend tells the controller to not run this automation, until
it is unset (or set to false). Defaults to false.</p>
</td>
</tr>
</table>
</td>
</tr>
<tr>
<td>
<code>status</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.ImageUpdateAutomationStatus">
ImageUpdateAutomationStatus
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.ImageUpdateAutomationSpec">ImageUpdateAutomationSpec
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.ImageUpdateAutomation">ImageUpdateAutomation</a>)
</p>
<p>ImageUpdateAutomationSpec defines the desired state of ImageUpdateAutomation</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>sourceRef</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.CrossNamespaceSourceReference">
CrossNamespaceSourceReference
</a>
</em>
</td>
<td>
<p>SourceRef refers to the resource giving access details
to a git repository.</p>
</td>
</tr>
<tr>
<td>
<code>git</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.GitSpec">
GitSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>GitSpec contains all the git-specific definitions. This is
technically optional, but in practice mandatory until there are
other kinds of source allowed.</p>
</td>
</tr>
<tr>
<td>
<code>interval</code><br>
<em>
<a href="https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
Kubernetes meta/v1.Duration
</a>
</em>
</td>
<td>
<p>Interval gives an lower bound for how often the automation
run should be attempted.</p>
</td>
</tr>
<tr>
<td>
<code>policySelector</code><br>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#labelselector-v1-meta">
Kubernetes meta/v1.LabelSelector
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>PolicySelector allows to filter applied policies based on labels.
By default includes all policies in namespace.</p>
</td>
</tr>
<tr>
<td>
<code>update</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.UpdateStrategy">
UpdateStrategy
</a>
</em>
</td>
<td>
<p>Update gives the specification for how to update the files in
the repository. This can be left empty, to use the default
value.</p>
</td>
</tr>
<tr>
<td>
<code>suspend</code><br>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Suspend tells the controller to not run this automation, until
it is unset (or set to false). Defaults to false.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.ImageUpdateAutomationStatus">ImageUpdateAutomationStatus
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.ImageUpdateAutomation">ImageUpdateAutomation</a>)
</p>
<p>ImageUpdateAutomationStatus defines the observed state of ImageUpdateAutomation</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>lastAutomationRunTime</code><br>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#time-v1-meta">
Kubernetes meta/v1.Time
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>LastAutomationRunTime records the last time the controller ran
this automation through to completion (even if no updates were
made).</p>
</td>
</tr>
<tr>
<td>
<code>lastPushCommit</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>LastPushCommit records the SHA1 of the last commit made by the
controller, for this automation object</p>
</td>
</tr>
<tr>
<td>
<code>lastPushTime</code><br>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#time-v1-meta">
Kubernetes meta/v1.Time
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>LastPushTime records the time of the last pushed change.</p>
</td>
</tr>
<tr>
<td>
<code>observedGeneration</code><br>
<em>
int64
</em>
</td>
<td>
<em>(Optional)</em>
</td>
</tr>
<tr>
<td>
<code>conditions</code><br>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#condition-v1-meta">
[]Kubernetes meta/v1.Condition
</a>
</em>
</td>
<td>
<em>(Optional)</em>
</td>
</tr>
<tr>
<td>
<code>observedPolicies</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.ObservedPolicies">
ObservedPolicies
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>ObservedPolicies is the list of observed ImagePolicies that were
considered by the ImageUpdateAutomation update process.</p>
</td>
</tr>
<tr>
<td>
<code>observedSourceRevision</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>ObservedPolicies []ObservedPolicy <code>json:&quot;observedPolicies,omitempty&quot;</code>
ObservedSourceRevision is the last observed source revision. This can be
used to determine if the source has been updated since last observation.</p>
</td>
</tr>
<tr>
<td>
<code>ReconcileRequestStatus</code><br>
<em>
<a href="https://pkg.go.dev/github.com/fluxcd/pkg/apis/meta#ReconcileRequestStatus">
github.com/fluxcd/pkg/apis/meta.ReconcileRequestStatus
</a>
</em>
</td>
<td>
<p>
(Members of <code>ReconcileRequestStatus</code> are embedded into this type.)
</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.ObservedPolicies">ObservedPolicies
(<code>map[string]./api/v1beta2.ImageRef</code> alias)</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.ImageUpdateAutomationStatus">ImageUpdateAutomationStatus</a>)
</p>
<p>ObservedPolicies is a map of policy name and ImageRef of their latest
ImageRef.</p>
<h3 id="image.toolkit.fluxcd.io/v1beta2.PushSpec">PushSpec
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.GitSpec">GitSpec</a>)
</p>
<p>PushSpec specifies how and where to push commits.</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>branch</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Branch specifies that commits should be pushed to the branch
named. The branch is created using <code>.spec.checkout.branch</code> as the
starting point, if it doesn&rsquo;t already exist.</p>
</td>
</tr>
<tr>
<td>
<code>refspec</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Refspec specifies the Git Refspec to use for a push operation.
If both Branch and Refspec are provided, then the commit is pushed
to the branch and also using the specified refspec.
For more details about Git Refspecs, see:
<a href="https://git-scm.com/book/en/v2/Git-Internals-The-Refspec">https://git-scm.com/book/en/v2/Git-Internals-The-Refspec</a></p>
</td>
</tr>
<tr>
<td>
<code>options</code><br>
<em>
map[string]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Options specifies the push options that are sent to the Git
server when performing a push operation. For details, see:
<a href="https://git-scm.com/docs/git-push#Documentation/git-push.txt---push-optionltoptiongt">https://git-scm.com/docs/git-push#Documentation/git-push.txt&mdash;push-optionltoptiongt</a></p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.SigningKey">SigningKey
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.CommitSpec">CommitSpec</a>)
</p>
<p>SigningKey references a Kubernetes secret that contains a GPG keypair</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>secretRef</code><br>
<em>
<a href="https://pkg.go.dev/github.com/fluxcd/pkg/apis/meta#LocalObjectReference">
github.com/fluxcd/pkg/apis/meta.LocalObjectReference
</a>
</em>
</td>
<td>
<p>SecretRef holds the name to a secret that contains a &lsquo;git.asc&rsquo; key
corresponding to the ASCII Armored file containing the GPG signing
keypair as the value. It must be in the same namespace as the
ImageUpdateAutomation.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.UpdateStrategy">UpdateStrategy
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.ImageUpdateAutomationSpec">ImageUpdateAutomationSpec</a>)
</p>
<p>UpdateStrategy is a union of the various strategies for updating
the Git repository. Parameters for each strategy (if any) can be
inlined here.</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>strategy</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1beta2.UpdateStrategyName">
UpdateStrategyName
</a>
</em>
</td>
<td>
<p>Strategy names the strategy to be used.</p>
</td>
</tr>
<tr>
<td>
<code>path</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Path to the directory containing the manifests to be updated.
Defaults to &lsquo;None&rsquo;, which translates to the root path
of the GitRepositoryRef.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1beta2.UpdateStrategyName">UpdateStrategyName
(<code>string</code> alias)</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1beta2.UpdateStrategy">UpdateStrategy</a>)
</p>
<p>UpdateStrategyName is the type for names that go in
.update.strategy. NB the value in the const immediately below.</p>
<div class="admonition note">
<p class="last">This page was automatically generated with <code>gen-crd-api-reference-docs</code></p>
</div>
