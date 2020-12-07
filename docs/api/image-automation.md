<h1>Image update automation API reference</h1>
<p>Packages:</p>
<ul class="simple">
<li>
<a href="#image.toolkit.fluxcd.io%2fv1alpha1">image.toolkit.fluxcd.io/v1alpha1</a>
</li>
</ul>
<h2 id="image.toolkit.fluxcd.io/v1alpha1">image.toolkit.fluxcd.io/v1alpha1</h2>
<p>Package v1alpha1 contains API types for the image v1alpha1 API
group. The types here are concerned with automated updates to git,
based on metadata from OCI image registries gathered by the
image-reflector-controller.</p>
Resource Types:
<ul class="simple"></ul>
<h3 id="image.toolkit.fluxcd.io/v1alpha1.CommitSpec">CommitSpec
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.ImageUpdateAutomationSpec">ImageUpdateAutomationSpec</a>)
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
<code>authorName</code><br>
<em>
string
</em>
</td>
<td>
<p>AuthorName gives the name to provide when making a commit</p>
</td>
</tr>
<tr>
<td>
<code>authorEmail</code><br>
<em>
string
</em>
</td>
<td>
<p>AuthorEmail gives the email to provide when making a commit</p>
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
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1alpha1.GitCheckoutSpec">GitCheckoutSpec
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.ImageUpdateAutomationSpec">ImageUpdateAutomationSpec</a>)
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
<code>gitRepositoryRef</code><br>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.18/#localobjectreference-v1-core">
Kubernetes core/v1.LocalObjectReference
</a>
</em>
</td>
<td>
<p>GitRepositoryRef refers to the resource giving access details
to a git repository to update files in.</p>
</td>
</tr>
<tr>
<td>
<code>branch</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Branch gives the branch to clone from the git repository. If
missing, it will be left to default; be aware this may give
indeterminate results.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1alpha1.ImageUpdateAutomation">ImageUpdateAutomation
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
<a href="#image.toolkit.fluxcd.io/v1alpha1.ImageUpdateAutomationSpec">
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
<code>checkout</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.GitCheckoutSpec">
GitCheckoutSpec
</a>
</em>
</td>
<td>
<p>Checkout gives the parameters for cloning the git repository,
ready to make changes.</p>
</td>
</tr>
<tr>
<td>
<code>interval</code><br>
<em>
<a href="https://godoc.org/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
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
<code>update</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.UpdateStrategy">
UpdateStrategy
</a>
</em>
</td>
<td>
<p>Update gives the specification for how to update the files in
the repository</p>
</td>
</tr>
<tr>
<td>
<code>commit</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.CommitSpec">
CommitSpec
</a>
</em>
</td>
<td>
<p>Commit specifies how to commit to the git repo</p>
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
<a href="#image.toolkit.fluxcd.io/v1alpha1.ImageUpdateAutomationStatus">
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
<h3 id="image.toolkit.fluxcd.io/v1alpha1.ImageUpdateAutomationSpec">ImageUpdateAutomationSpec
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.ImageUpdateAutomation">ImageUpdateAutomation</a>)
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
<code>checkout</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.GitCheckoutSpec">
GitCheckoutSpec
</a>
</em>
</td>
<td>
<p>Checkout gives the parameters for cloning the git repository,
ready to make changes.</p>
</td>
</tr>
<tr>
<td>
<code>interval</code><br>
<em>
<a href="https://godoc.org/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
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
<code>update</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.UpdateStrategy">
UpdateStrategy
</a>
</em>
</td>
<td>
<p>Update gives the specification for how to update the files in
the repository</p>
</td>
</tr>
<tr>
<td>
<code>commit</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.CommitSpec">
CommitSpec
</a>
</em>
</td>
<td>
<p>Commit specifies how to commit to the git repo</p>
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
<h3 id="image.toolkit.fluxcd.io/v1alpha1.ImageUpdateAutomationStatus">ImageUpdateAutomationStatus
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.ImageUpdateAutomation">ImageUpdateAutomation</a>)
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
<code>ReconcileRequestStatus</code><br>
<em>
<a href="https://godoc.org/github.com/fluxcd/pkg/apis/meta#ReconcileRequestStatus">
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
<h3 id="image.toolkit.fluxcd.io/v1alpha1.SettersStrategy">SettersStrategy
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.UpdateStrategy">UpdateStrategy</a>)
</p>
<p>SettersStrategy specifies how to use kyaml setters to update the
git repository.</p>
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
<code>paths</code><br>
<em>
[]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Paths gives all paths that should be subject to updates using
setters. If missing, the path <code>.</code> (the root of the git
repository) is assumed.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="image.toolkit.fluxcd.io/v1alpha1.UpdateStrategy">UpdateStrategy
</h3>
<p>
(<em>Appears on:</em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.ImageUpdateAutomationSpec">ImageUpdateAutomationSpec</a>)
</p>
<p>UpdateStrategy is a union of the various strategies for updating
the git repository.</p>
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
<code>setters</code><br>
<em>
<a href="#image.toolkit.fluxcd.io/v1alpha1.SettersStrategy">
SettersStrategy
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Setters if present means update workloads using setters, via
fields marked in the files themselves.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<div class="admonition note">
<p class="last">This page was automatically generated with <code>gen-crd-api-reference-docs</code></p>
</div>
