package am.ghi.ide;

import com.intellij.codeInsight.intention.IntentionAction;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.ui.popup.JBPopupFactory;
import com.intellij.psi.PsiFile;
import org.jetbrains.annotations.NotNull;
import java.util.List;

public abstract class GhiImportIntention implements IntentionAction {
    private final boolean qualified;
    protected GhiImportIntention(boolean qualified){this.qualified=qualified;}
    @Override public @NotNull String getText(){return qualified?"Replace with Ghi type import":"Import Ghi type";}
    @Override public @NotNull String getFamilyName(){return "Ghi type imports";}
    @Override public boolean startInWriteAction(){return false;}
    private List<GhiTypeImports.Choice> choices(PsiFile file,GhiTypeImports.Site site){
        if(site==null)return List.of();
        return GhiTypeImports.candidates(file,qualified?null:site.text()).stream().filter(choice->!qualified||choice.path().equals(site.text())).toList();
    }
    @Override public boolean isAvailable(@NotNull Project project,Editor editor,PsiFile file){
        return editor!=null&&file!=null&&file.getLanguage()==GhiLanguage.INSTANCE&&!choices(file,GhiTypeImports.site(file,editor.getCaretModel().getOffset(),qualified)).isEmpty();
    }
    @Override public void invoke(@NotNull Project project,Editor editor,PsiFile file){
        var site=GhiTypeImports.site(file,editor.getCaretModel().getOffset(),qualified);var candidates=choices(file,site);
        if(candidates.size()==1)GhiTypeImports.apply(file,editor,site,candidates.getFirst());
        else if(!candidates.isEmpty())JBPopupFactory.getInstance().createPopupChooserBuilder(candidates).setTitle("Import Ghi type")
            .setItemChosenCallback(choice->GhiTypeImports.apply(file,editor,site,choice)).createPopup().showInBestPositionFor(editor);
    }
    public static final class Import extends GhiImportIntention {public Import(){super(false);}}
    public static final class Shorten extends GhiImportIntention {public Shorten(){super(true);}}
}
