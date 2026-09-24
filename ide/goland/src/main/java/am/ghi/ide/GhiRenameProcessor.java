package am.ghi.ide;
import com.intellij.refactoring.rename.RenamePsiElementProcessor;
import com.intellij.openapi.editor.Editor;
import com.intellij.psi.PsiElement;
import org.jetbrains.annotations.NotNull;
import java.util.Map;
public final class GhiRenameProcessor extends RenamePsiElementProcessor {
    @Override public boolean canProcessElement(@NotNull PsiElement element){return element instanceof GhiIdentifier;}
    @Override public PsiElement substituteElementToRename(@NotNull PsiElement element,Editor editor){
        PsiElement target=GhiSymbols.forFile(element.getContainingFile()).resolve(element.getContainingFile(),element.getTextOffset());
        return target instanceof GhiIdentifier?target:null;
    }
    @Override public void prepareRenaming(@NotNull PsiElement element,@NotNull String newName,@NotNull Map<PsiElement,String> renames){
        var model=GhiSymbols.forFile(element.getContainingFile());var target=model.symbolAt(element.getContainingFile(),element.getTextOffset());
        if(target!=null)for(var member:model.methodFamily(target))if(member.psi()!=null)renames.put(member.psi(),newName);
    }
    @Override public void findExistingNameConflicts(@NotNull PsiElement element,@NotNull String newName,@NotNull com.intellij.util.containers.MultiMap<PsiElement,String> conflicts){
        var model=GhiSymbols.forFile(element.getContainingFile());var target=model.symbolAt(element.getContainingFile(),element.getTextOffset());
        if(target==null)return;
        if(model.ambiguousStructuralFamily(target))conflicts.putValue(element,"Cannot safely rename this structural interface family: generic, alias or compound function signatures need full compiler type resolution.");
        for(var renamed:model.methodFamily(target))for(var candidate:model.symbols)if(candidate!=renamed&&candidate.name.equals(newName)){
            // Refuse capture both at the selected declaration and across the whole contract family.
            if(candidate.file==renamed.file||candidate.scope==renamed.scope
                ||(renamed.scope.parent==null&&candidate.scope.parent==null&&model.sources.get(candidate.file).namespace.equals(model.sources.get(renamed.file).namespace))
                ||(renamed.scope.owner!=null&&candidate.scope.owner!=null&&model.related(renamed.scope.owner,candidate.scope.owner)))
                conflicts.putValue(candidate.psi(),"Renaming to '"+newName+"' may capture an existing Ghi symbol.");
        }
    }
    @Override public boolean isToSearchInComments(@NotNull PsiElement element){return false;}
    @Override public boolean isToSearchForTextOccurrences(@NotNull PsiElement element){return false;}
}
