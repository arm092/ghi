package am.ghi.ide;
import com.intellij.refactoring.rename.RenamePsiElementProcessor;
import com.intellij.openapi.editor.Editor;
import com.intellij.psi.PsiElement;
import org.jetbrains.annotations.NotNull;
public final class GhiRenameProcessor extends RenamePsiElementProcessor {
    @Override public boolean canProcessElement(@NotNull PsiElement element){return element instanceof GhiIdentifier;}
    @Override public PsiElement substituteElementToRename(@NotNull PsiElement element,Editor editor){
        return GhiSymbols.forFile(element.getContainingFile()).resolve(element.getContainingFile(),element.getTextOffset());
    }
    @Override public void findExistingNameConflicts(@NotNull PsiElement element,@NotNull String newName,@NotNull com.intellij.util.containers.MultiMap<PsiElement,String> conflicts){
        var model=GhiSymbols.forFile(element.getContainingFile());var target=model.symbolAt(element.getContainingFile(),element.getTextOffset());
        if(target==null)return;
        if(target.kind.equals("func")&&target.scope.owner!=null)for(var candidate:model.symbols)
            if(candidate!=target&&candidate.kind.equals("func")&&candidate.name.equals(target.name)&&candidate.scope.owner!=null
                &&(model.related(target.scope.owner,candidate.scope.owner)||candidate.scope.owner.kind.equals("interface")||target.scope.owner.kind.equals("interface")))
                conflicts.putValue(candidate.psi(),"Rename of an override or interface method family is not supported; update the contract and implementations together.");
        for(var candidate:model.symbols)if(candidate!=target&&candidate.name.equals(newName)){
            // Conservatively refuse potential capture in the same file or declaration scope.
            if(candidate.file==target.file||candidate.scope==target.scope||(target.scope.parent==null&&candidate.scope.parent==null&&model.sources.get(candidate.file).namespace.equals(model.sources.get(target.file).namespace)))
                conflicts.putValue(candidate.psi(),"Renaming to '"+newName+"' may capture an existing Ghi symbol.");
        }
    }
    @Override public boolean isToSearchInComments(@NotNull PsiElement element){return false;}
    @Override public boolean isToSearchForTextOccurrences(@NotNull PsiElement element){return false;}
}


