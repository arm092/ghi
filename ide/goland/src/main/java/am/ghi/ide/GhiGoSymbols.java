package am.ghi.ide;

import com.goide.psi.*;
import com.goide.psi.impl.GoPackage;
import com.goide.sdk.GoPackageUtil;
import com.goide.sdk.GoSdkService;
import com.intellij.openapi.module.ModuleUtilCore;
import com.intellij.openapi.project.DumbService;
import com.intellij.psi.*;
import java.util.*;

/** Uses the configured GoLand SDK/module graph and real Go PSI declarations. */
final class GhiGoSymbols {
    static List<String> importPaths(PsiFile context,String prefix){
        if(!prefix.startsWith("go:"))return List.of();
        String relative=prefix.substring(3);int slash=relative.lastIndexOf('/');String parent=slash<0?"":relative.substring(0,slash+1);
        if(Arrays.asList(parent.split("/")).contains(".."))return List.of();
        var module=ModuleUtilCore.findModuleForPsiElement(context);var sdk=GoSdkService.getInstance(context.getProject()).getSdk(module);
        var root=sdk==null?null:sdk.getSdkRoot();var directory=root==null?null:root.findFileByRelativePath("src/"+parent);
        List<String> result=new ArrayList<>(GhiGoModules.importPaths(context,prefix));
        if(directory==null){Collections.sort(result);return result;}
        for(var child:directory.getChildren())if(child.isDirectory()&&!Set.of("internal","vendor","testdata","cmd").contains(child.getName())){
            String path="go:"+parent+child.getName();if(path.startsWith(prefix))result.add(path);
        }
        Collections.sort(result);return result;
    }
    static List<GhiSymbols.Symbol> packageMembers(String path,PsiFile context){
        if(DumbService.isDumb(context.getProject()))return List.of();
        var module=ModuleUtilCore.findModuleForPsiElement(context);
        List<GoFile> files=new ArrayList<>();
        var locked=GhiGoModules.resolve(context,path);
        if(locked.pinned()){
            if(locked.directory()==null)return List.of();
            collectDirectory(context,locked.directory(),files);
        }else{
            for(var pkg:GoPackageUtil.findByImportPath(path,context.getProject(),module,ResolveState.initial()))pkg.processBuildableFiles(context,module,candidate->{
                if((Object)candidate instanceof GoFile file)files.add(file);return true;
            });
            // Ghi projects need no go.mod to use their configured SDK sources.
            if(files.isEmpty()){
                var sdk=GoSdkService.getInstance(context.getProject()).getSdk(module);
                var root=sdk==null?null:sdk.getSdkRoot();var directory=root==null?null:root.findFileByRelativePath("src/"+path);
                if(directory!=null)collectDirectory(context,directory,files);
            }
        }
        Map<String,GhiSymbols.Symbol> result=new LinkedHashMap<>();
        for(GoFile file:files){
            if(file.getName().endsWith("_test.go"))continue;
            for(GoNamedElement element:file.getFunctions())add(result,element,context,path);
            for(GoNamedElement element:file.getTypes())add(result,element,context,path);
            for(GoNamedElement element:file.getVars())add(result,element,context,path);
            for(GoNamedElement element:file.getConstants())add(result,element,context,path);
        }
        return new ArrayList<>(result.values());
    }
    private static void collectDirectory(PsiFile context,com.intellij.openapi.vfs.VirtualFile directory,List<GoFile> files){
        var module=ModuleUtilCore.findModuleForPsiElement(context);
        var psi=PsiManager.getInstance(context.getProject()).findDirectory(directory);
        if(psi!=null)for(var pkg:GoPackage.in(psi,module))pkg.processBuildableFiles(context,module,candidate->{
            if((Object)candidate instanceof GoFile file)files.add(file);return true;
        });
    }
    private static void add(Map<String,GhiSymbols.Symbol> result,GoNamedElement element,PsiFile context,String path){
        if(element.isPublic()&&element.getName()!=null)result.putIfAbsent(element.getName(),symbol(element,context,path));
    }
    private static GhiSymbols.Symbol symbol(GoNamedElement element,PsiFile context,String path){
        String kind=element instanceof GoTypeSpec?"go type":element instanceof GoFunctionOrMethodDeclaration?"func":"go value";
        var symbol=new GhiSymbols.Symbol(element.getName(),kind,-1,context,null);symbol.external=element;symbol.importNamespace="go:"+path;
        if(element instanceof GoFunctionOrMethodDeclaration function){
            var signature=function.getSignature();
            if(signature!=null){
                for(var declaration:signature.getParameters().getParameterDeclarationList()){
                    var definitions=declaration.getParamDefinitionList();String type=declaration.getType()==null?"":declaration.getType().getText();
                    if(declaration.isVariadic())type="..."+type;
                    if(definitions.isEmpty())symbol.parameters.add(type);
                    else for(var definition:definitions)symbol.parameters.add(definition.getName()+" "+type);
                }
                if(signature.getResult()!=null)symbol.type=signature.getResult().getText();
            }
        }
        return symbol;
    }
    static GhiSymbols.Symbol resultType(GhiSymbols.Symbol symbol){
        if(symbol.external instanceof GoTypeSpec)return symbol;
        GoType type=null;
        if(symbol.external instanceof GoFunctionOrMethodDeclaration function){
            var result=function.getSignature().getResult();
            if(result!=null){type=result.getType();if(type==null&&result.getParameters()!=null&&!result.getParameters().getParameterDeclarationList().isEmpty())type=result.getParameters().getParameterDeclarationList().getFirst().getType();}
        }else if(symbol.external instanceof GoNamedElement named)type=named.findSiblingType();
        if(type==null)return null;
        while(type instanceof GoPointerType pointer)type=pointer.getType();
        PsiElement resolved=type.contextlessResolve();
        if(resolved instanceof GoTypeSpec spec)return symbol(spec,symbol.file,symbol.importNamespace.substring(3));
        String localName=GhiGenericTypes.base(type.getText());
        return packageMembers(symbol.importNamespace.substring(3),symbol.file).stream().filter(candidate->candidate.external instanceof GoTypeSpec&&candidate.name.equals(localName)).findFirst().orElse(null);
    }
    static List<GhiSymbols.Symbol> members(GhiSymbols.Symbol symbol){
        var type=resultType(symbol);if(type==null||!(type.external instanceof GoTypeSpec spec))return List.of();
        String path=type.importNamespace.substring(3);Map<String,GhiSymbols.Symbol> result=new LinkedHashMap<>();
        for(var method:spec.getAllMethods())add(result,method,type.file,path);
        // A lock-pinned cache directory need not belong to GoLand's module index.
        // Read its buildable Go PSI directly for exact receiver declarations.
        List<GoFile> files=new ArrayList<>();collectDirectory(type.file,spec.getContainingFile().getVirtualFile().getParent(),files);
        for(GoFile file:files)for(var method:file.getMethods()){
            var receiver=method.getReceiverType();
            if(receiver!=null&&GhiGenericTypes.base(receiver.getText().replaceFirst("^\\*","")).equals(spec.getName()))add(result,method,type.file,path);
        }
        var declaration=spec.getSpecType();var underlying=declaration==null?null:declaration.getType();
        if(underlying!=null)underlying=underlying.getContextlessUnderlyingType();
        if(underlying instanceof GoStructType structure)for(var field:structure.getFieldDefinitions())add(result,field,type.file,path);
        return new ArrayList<>(result.values());
    }
}
